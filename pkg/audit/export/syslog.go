package export

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/liyang/weave/pkg/audit"
)

// Syslog facility / severity constants per RFC 3164. Only the subset an
// audit pipeline is likely to use is exposed; the full space is documented
// in RFC 3164 §4.1.1.
const (
	SyslogFacilityKernel = 0
	SyslogFacilityUser   = 1
	SyslogFacilityDaemon = 3
	SyslogFacilityAuth   = 4
	SyslogFacilitySyslog = 5
	SyslogFacilityLocal0 = 16
	SyslogFacilityLocal1 = 17
	SyslogFacilityLocal7 = 23

	SyslogSeverityEmerg   = 0
	SyslogSeverityAlert   = 1
	SyslogSeverityCrit    = 2
	SyslogSeverityErr     = 3
	SyslogSeverityWarning = 4
	SyslogSeverityNotice  = 5
	SyslogSeverityInfo    = 6
	SyslogSeverityDebug   = 7
)

// SyslogOptions controls how audit events are framed onto a syslog
// transport. Zero values fall back to sensible defaults (user/info, local
// hostname, "weave" as the app tag) so callers can wire a minimal config.
type SyslogOptions struct {
	Facility int
	Severity int
	Hostname string
	AppName  string
}

// SyslogExporter frames audit events as RFC 3164 syslog messages and
// writes them to an arbitrary io.Writer. The default framing is:
//
//	<priority>Mon DD HH:MM:SS hostname tag: <json payload>
//
// One audit event == one syslog line (trailing \n included). The full
// AuditEvent is encoded as JSON and appended as the MSG part so
// downstream SIEMs can parse structured data rather than regex the
// free-form message.
type SyslogExporter struct {
	mu   sync.Mutex
	w    io.Writer
	opts SyslogOptions
}

// NewSyslogExporterWriter wraps an arbitrary io.Writer (test buffer, file,
// TCP/UDP conn). A nil writer is accepted so the struct is still
// introspectable (Name()) without a live transport.
func NewSyslogExporterWriter(w io.Writer, opts SyslogOptions) *SyslogExporter {
	return &SyslogExporter{w: w, opts: fillSyslogDefaults(opts)}
}

// NewSyslogExporterUDP dials a UDP syslog endpoint (e.g. "siem:514") and
// returns an exporter writing RFC 3164 frames to that conn. The caller
// owns no conn resources — the exporter takes ownership and does NOT
// currently expose a Close; the connection lives for the process lifetime.
func NewSyslogExporterUDP(addr string, opts SyslogOptions) (*SyslogExporter, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("syslog udp dial %s: %w", addr, err)
	}
	return NewSyslogExporterWriter(conn, opts), nil
}

// NewSyslogExporterTCP dials a TCP syslog endpoint. Same ownership rules
// as NewSyslogExporterUDP.
func NewSyslogExporterTCP(addr string, opts SyslogOptions) (*SyslogExporter, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("syslog tcp dial %s: %w", addr, err)
	}
	return NewSyslogExporterWriter(conn, opts), nil
}

func (e *SyslogExporter) Name() string { return "syslog" }

func (e *SyslogExporter) Export(_ context.Context, batch []audit.AuditEvent) error {
	if len(batch) == 0 || e.w == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	priority := e.opts.Facility*8 + e.opts.Severity
	for i := range batch {
		payload, err := json.Marshal(&batch[i])
		if err != nil {
			return err
		}
		ts := batch[i].Timestamp
		if ts.IsZero() {
			// Defensive — callers should always stamp this.
			continue
		}
		// RFC 3164 timestamp: "Mon DD HH:MM:SS" with a single-space pad for
		// single-digit days (Go's "_2" directive).
		line := fmt.Sprintf("<%d>%s %s %s: %s\n",
			priority,
			ts.Format("Jan _2 15:04:05"),
			e.opts.Hostname,
			e.opts.AppName,
			string(payload),
		)
		if _, err := io.WriteString(e.w, line); err != nil {
			return err
		}
	}
	return nil
}

func fillSyslogDefaults(opts SyslogOptions) SyslogOptions {
	if opts.Facility < 0 || opts.Facility > 23 {
		opts.Facility = SyslogFacilityUser
	}
	if opts.Severity < 0 || opts.Severity > 7 {
		opts.Severity = SyslogSeverityInfo
	}
	if strings.TrimSpace(opts.Hostname) == "" {
		if h, err := os.Hostname(); err == nil {
			opts.Hostname = h
		} else {
			opts.Hostname = "weave"
		}
	}
	if strings.TrimSpace(opts.AppName) == "" {
		opts.AppName = "weave"
	}
	return opts
}
