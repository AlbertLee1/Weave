package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_OutgoingLinkTypes_LinkTypeSideV2 locks the Foundry
// LinkTypeSideV2 wire contract onto the outgoing/incoming link-type
// read surfaces.
//
// Foundry's LinkTypeSideV2 is:
//
//	{apiName, displayName, status, objectTypeApiName, cardinality,
//	 foreignKeyPropertyApiName?, linkTypeRid}
//
// where objectTypeApiName is the object type at the LINKED (far) end
// of the link relative to the queried object type. Weave historically
// deviated in two ways on GET /objectTypes/{ot}/outgoingLinkTypes:
//
//  1. objectTypeApiName carried the SOURCE type (the queried type
//     itself) and the far end was exiled to the home-grown
//     linkedObjectTypeApiName key;
//  2. the RID was published under `rid` instead of `linkTypeRid`,
//     and `status` / `foreignKeyPropertyApiName` were missing.
//
// Transition policy (this commit): the Foundry fields are added with
// correct semantics; the legacy `rid` and `linkedObjectTypeApiName`
// keys remain as deprecated aliases carrying the SAME values as
// `linkTypeRid` / `objectTypeApiName` so existing web consumers keep
// rendering while they migrate.
//
// Scenarios (Given → When → Then):
//
//   - Given Employee --worksIn--> Department (FK link) and
//     Employee --assignedTo--> Project (M2M link),
//     When GET /objectTypes/Employee/outgoingLinkTypes,
//     Then each entry's objectTypeApiName is the FAR end
//     (Department / Project), linkTypeRid carries the link RID and
//     status defaults to ACTIVE.
//
//   - FK-backed links expose foreignKeyPropertyApiName (the FK
//     property on the source side of the link); M2M links omit it.
//
//   - Deprecated aliases: rid == linkTypeRid and
//     linkedObjectTypeApiName == objectTypeApiName.
//
//   - Incoming direction obeys the same "far end" semantics: for
//     GET /objectTypes/Department/incomingLinkTypes the far end of
//     worksIn is Employee (the source), so objectTypeApiName must be
//     Employee.
func TestBDD_OutgoingLinkTypes_LinkTypeSideV2(t *testing.T) {
	const (
		ontRID  = "ri.ontology.main.ontology.1"
		otEmp   = "ri.ontology.main.object-type.emp"
		otDept  = "ri.ontology.main.object-type.dept"
		otProj  = "ri.ontology.main.object-type.proj"
		ltWorks = "ri.ontology.main.link-type.worksIn"
		ltAssig = "ri.ontology.main.link-type.assignedTo"
	)

	newServer := func(t *testing.T) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "hr", DisplayName: "HR",
		})
		repo.objectTypes = append(repo.objectTypes,
			oms.ObjectType{RID: otEmp, OntologyRID: ontRID, APIName: "Employee", PrimaryKey: "id"},
			oms.ObjectType{RID: otDept, OntologyRID: ontRID, APIName: "Department", PrimaryKey: "id"},
			oms.ObjectType{RID: otProj, OntologyRID: ontRID, APIName: "Project", PrimaryKey: "id"},
		)
		repo.linkTypes = append(repo.linkTypes,
			oms.LinkType{
				RID: ltWorks, OntologyRID: ontRID, APIName: "worksIn",
				DisplayName:      "Works In",
				SourceObjectType: otEmp, TargetObjectType: otDept,
				Cardinality:      "MANY_TO_ONE",
				ForeignKeyConfig: json.RawMessage(`{"sourceProperty":"departmentId","targetProperty":"id"}`),
				IsRequired:       true,
			},
			oms.LinkType{
				RID: ltAssig, OntologyRID: ontRID, APIName: "assignedTo",
				DisplayName:      "Assigned To",
				SourceObjectType: otEmp, TargetObjectType: otProj,
				Cardinality:     "MANY_TO_MANY",
				JoinTableConfig: json.RawMessage(`{"table":"emp_proj"}`),
			},
		)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes",
			handler.ListOutgoingLinkTypes)
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/incomingLinkTypes",
			handler.ListIncomingLinkTypes)
		return r
	}

	listLinks := func(t *testing.T, r *chi.Mux, otAPIName, direction string) map[string]map[string]interface{} {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/objectTypes/"+otAPIName+"/"+direction, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v; body=%s", err, rec.Body.String())
		}
		byAPIName := make(map[string]map[string]interface{}, len(resp.Data))
		for _, lt := range resp.Data {
			name, _ := lt["apiName"].(string)
			byAPIName[name] = lt
		}
		return byAPIName
	}

	t.Run("objectTypeApiName is the linked (far) end, with linkTypeRid and status", func(t *testing.T) {
		r := newServer(t)
		links := listLinks(t, r, "Employee", "outgoingLinkTypes")
		worksIn, ok := links["worksIn"]
		if !ok {
			t.Fatalf("worksIn missing from response: %v", links)
		}
		if got := worksIn["objectTypeApiName"]; got != "Department" {
			t.Errorf("worksIn objectTypeApiName=%v, want Department (the linked side, Foundry LinkTypeSideV2 semantics)", got)
		}
		if got := worksIn["linkTypeRid"]; got != ltWorks {
			t.Errorf("worksIn linkTypeRid=%v, want %s", got, ltWorks)
		}
		if got := worksIn["status"]; got != "ACTIVE" {
			t.Errorf("worksIn status=%v, want ACTIVE", got)
		}
		assignedTo, ok := links["assignedTo"]
		if !ok {
			t.Fatalf("assignedTo missing from response: %v", links)
		}
		if got := assignedTo["objectTypeApiName"]; got != "Project" {
			t.Errorf("assignedTo objectTypeApiName=%v, want Project", got)
		}
	})

	t.Run("FK link exposes foreignKeyPropertyApiName; M2M omits it", func(t *testing.T) {
		r := newServer(t)
		links := listLinks(t, r, "Employee", "outgoingLinkTypes")
		worksIn := links["worksIn"]
		if worksIn == nil {
			t.Fatal("worksIn missing")
		}
		if got := worksIn["foreignKeyPropertyApiName"]; got != "departmentId" {
			t.Errorf("worksIn foreignKeyPropertyApiName=%v, want departmentId", got)
		}
		assignedTo := links["assignedTo"]
		if assignedTo == nil {
			t.Fatal("assignedTo missing")
		}
		if _, present := assignedTo["foreignKeyPropertyApiName"]; present {
			t.Errorf("assignedTo (MANY_TO_MANY) must omit foreignKeyPropertyApiName, got %v",
				assignedTo["foreignKeyPropertyApiName"])
		}
	})

	t.Run("deprecated aliases rid and linkedObjectTypeApiName carry the new values", func(t *testing.T) {
		r := newServer(t)
		links := listLinks(t, r, "Employee", "outgoingLinkTypes")
		worksIn := links["worksIn"]
		if worksIn == nil {
			t.Fatal("worksIn missing")
		}
		if got := worksIn["rid"]; got != worksIn["linkTypeRid"] {
			t.Errorf("alias rid=%v must equal linkTypeRid=%v during the transition", got, worksIn["linkTypeRid"])
		}
		if got := worksIn["linkedObjectTypeApiName"]; got != worksIn["objectTypeApiName"] {
			t.Errorf("alias linkedObjectTypeApiName=%v must equal objectTypeApiName=%v during the transition",
				got, worksIn["objectTypeApiName"])
		}
		// Weave extension consumed by the web LinkTypesPanel badge — must
		// survive the reshape.
		if got := worksIn["required"]; got != true {
			t.Errorf("required=%v, want true", got)
		}
	})

	t.Run("incoming direction reports the far end (the source) under objectTypeApiName", func(t *testing.T) {
		r := newServer(t)
		links := listLinks(t, r, "Department", "incomingLinkTypes")
		worksIn, ok := links["worksIn"]
		if !ok {
			t.Fatalf("worksIn missing from incoming response: %v", links)
		}
		if got := worksIn["objectTypeApiName"]; got != "Employee" {
			t.Errorf("incoming worksIn objectTypeApiName=%v, want Employee (the far end seen from Department)", got)
		}
		if got := worksIn["linkTypeRid"]; got != ltWorks {
			t.Errorf("incoming worksIn linkTypeRid=%v, want %s", got, ltWorks)
		}
		if got := worksIn["status"]; got != "ACTIVE" {
			t.Errorf("incoming worksIn status=%v, want ACTIVE", got)
		}
	})
}
