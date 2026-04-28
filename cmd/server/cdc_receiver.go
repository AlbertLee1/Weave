package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/pipeline/cdc"
)

// cdcConfigEnv loads the CDC mapping configuration from the environment.
// Two shapes are accepted to keep deployment simple:
//
//   - WEAVE_CDC_MAPPINGS — inline JSON array of {schema,table,ontology,
//     objectType,primaryKeyColumns,propertyColumns} entries.
//   - WEAVE_CDC_MAPPINGS_FILE — path to a JSON file with the same shape.
//
// Missing / empty config disables CDC silently — same shape as the rest
// of the optional integrations (LDAP, OIDC, SAML, ...). Returns
// (nil, nil) when CDC is disabled, (nil, err) when configured but
// invalid, or (config, nil) when ready.
func cdcConfigFromEnv() (*cdc.Config, error) {
	raw := strings.TrimSpace(os.Getenv("WEAVE_CDC_MAPPINGS"))
	if raw == "" {
		path := strings.TrimSpace(os.Getenv("WEAVE_CDC_MAPPINGS_FILE"))
		if path == "" {
			return nil, nil
		}
		buf, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cdc: read mappings file %q: %w", path, err)
		}
		raw = string(buf)
	}
	var entries []struct {
		Schema                string            `json:"schema"`
		Table                 string            `json:"table"`
		OntologyAPIName       string            `json:"ontology"`
		ObjectType            string            `json:"objectType"`
		PrimaryKeyColumns     []string          `json:"primaryKeyColumns"`
		PropertyColumns       map[string]string `json:"propertyColumns"`
		IncludeNullProperties bool              `json:"includeNullProperties,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("cdc: parse mappings JSON: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	cfg := &cdc.Config{Tables: make([]cdc.TableMapping, 0, len(entries))}
	for _, e := range entries {
		cfg.Tables = append(cfg.Tables, cdc.TableMapping{
			Schema:                e.Schema,
			Table:                 e.Table,
			OntologyAPIName:       e.OntologyAPIName,
			ObjectType:            e.ObjectType,
			PrimaryKeyColumns:     e.PrimaryKeyColumns,
			PropertyColumns:       e.PropertyColumns,
			IncludeNullProperties: e.IncludeNullProperties,
		})
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// startCDCReceiver wires a PGSource → Decoder → Receiver chain that
// publishes EditBatches onto the funnel publisher. It returns a stop
// function the caller invokes during graceful shutdown to drain the
// receiver's in-flight transaction and close the replication
// connection.
//
// All wiring is opt-in:
//   - WEAVE_CDC_DSN provides the replication-enabled DSN. The function
//     refuses to add `replication=database` automatically — operators
//     must declare it explicitly so the wrong DSN never spins up an
//     extra walsender by accident.
//   - WEAVE_CDC_SLOT names the logical replication slot.
//   - WEAVE_CDC_PUBLICATION names the pgoutput publication.
//   - WEAVE_CDC_MAPPINGS / _FILE configures the table → ObjectType
//     routing.
//
// Returns (nop, nil) when CDC is disabled. Returns (nop, err) when
// CDC is configured but the connection / slot setup fails — main
// logs the error and continues so a misconfigured CDC never takes
// down the rest of the server.
func startCDCReceiver(ctx context.Context, publisher *funnel.Publisher) (stop func(), err error) {
	noop := func() {}
	if publisher == nil {
		return noop, nil
	}
	dsn := strings.TrimSpace(os.Getenv("WEAVE_CDC_DSN"))
	if dsn == "" {
		return noop, nil
	}
	slot := strings.TrimSpace(os.Getenv("WEAVE_CDC_SLOT"))
	if slot == "" {
		slot = "weave_cdc"
	}
	pub := strings.TrimSpace(os.Getenv("WEAVE_CDC_PUBLICATION"))
	if pub == "" {
		pub = "weave_cdc_pub"
	}
	cfg, err := cdcConfigFromEnv()
	if err != nil {
		return noop, fmt.Errorf("cdc: load mappings: %w", err)
	}
	if cfg == nil {
		return noop, errors.New("cdc: WEAVE_CDC_DSN set but no mappings configured (set WEAVE_CDC_MAPPINGS or WEAVE_CDC_MAPPINGS_FILE)")
	}

	connectCtx, cancelConnect := context.WithTimeout(ctx, 15*time.Second)
	defer cancelConnect()
	conn, err := pgconn.Connect(connectCtx, dsn)
	if err != nil {
		return noop, fmt.Errorf("cdc: pgconn.Connect: %w", err)
	}

	source, err := cdc.NewPGSource(conn, cdc.PGSourceOptions{
		SlotName:          slot,
		PublicationName:   pub,
		KeepaliveInterval: cdcKeepaliveFromEnv(),
	})
	if err != nil {
		_ = conn.Close(context.Background())
		return noop, err
	}
	if err := source.Start(ctx); err != nil {
		_ = conn.Close(context.Background())
		return noop, fmt.Errorf("cdc: start replication: %w", err)
	}

	receiver, err := cdc.NewReceiver(source, &cdc.FunnelPublisher{Inner: publisher}, cfg, cdc.Options{
		UserID: "cdc",
		Logger: log.Printf,
	})
	if err != nil {
		_ = conn.Close(context.Background())
		return noop, err
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := receiver.Run(runCtx); err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			log.Printf("[cdc] receiver exited: %v", err)
			return
		}
		log.Printf("[cdc] receiver stopped")
	}()
	log.Printf("[cdc] receiver started: slot=%s publication=%s tables=%d", slot, pub, len(cfg.Tables))

	return func() {
		cancelRun()
		wg.Wait()
	}, nil
}

func cdcKeepaliveFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WEAVE_CDC_KEEPALIVE_SECONDS"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
