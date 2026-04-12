package seed_northwind

// Minimal Northwind subset wired for Playwright E2E coverage. The full 11
// table catalogue lives in testdata/northwind/ and is consumed by the
// heavier Go-level Northwind harness in test/northwind/; this file
// intentionally picks only the 3 object types + 2 link types Playwright
// specs need to exercise Browser, search/filter, and linkedObjects flows.
//
// Seed rows are hand-written so the assertions in web/e2e/*.spec.ts can
// pin on specific values (e.g. a customer named "Alice Chen" in country
// "usa"). Keep them small (< 20 rows total) — the whole seed must run in
// under 30s end-to-end (US-030 acceptance).

type schema struct {
	APIName           string
	DisplayName       string
	PluralDisplayName string
	PrimaryKey        string
	TitleProperty     string
	Properties        []schemaProperty
	SeedRows          []map[string]interface{}
}

type schemaProperty struct {
	APIName      string
	DisplayName  string
	BaseType     string
	IsSearchable bool
	IsSortable   bool
	// Analyzer mirrors the Foundry TypeConfig.analyzer hint:
	//   "not_analyzed" → KeywordField (exact-case term match)
	//   "standard" / empty → TextField (English stemmed)
	// Foreign-key properties (e.g. customerID) need "not_analyzed" so
	// TermQuery-based FK resolution (pkg/links/fk_resolver.go) actually hits.
	Analyzer string
}

type fkConfigDef struct {
	SourceProperty string `json:"sourceProperty"`
	TargetProperty string `json:"targetProperty"`
}

type linkDef struct {
	APIName     string
	DisplayName string
	Source      string // object type apiName
	Target      string // object type apiName
	Cardinality string // ONE_TO_MANY / MANY_TO_MANY / ...
	// FK names the source + target properties for FK-backed resolution.
	// Empty means the seed writes no foreign_key_config row, matching the
	// original US-030 behaviour (the link is metadata-only and cannot be
	// traversed by ObjectSet searchAround / withProperties).
	FK *fkConfigDef
}

// securityPolicyDef describes a PROPERTY-scope or OBJECT-scope policy to
// seed into the security_policies table. Used by US-081 (policy-column-hiding)
// so the Playwright spec can verify per-role column visibility end-to-end.
type securityPolicyDef struct {
	RID           string // stable RID for the policy row
	ObjectTypeAPI string // resolved to RID at seed time
	PolicyType    string // "OBJECT" or "PROPERTY"
	RulesJSON     string // JSON-encoded []security.Rule
}

// northwindSecurityPolicies returns the security policies seeded for E2E
// Playwright specs. The policy-column-hiding spec (US-081) depends on the
// employee PROPERTY policy to verify that salary is hidden from peer users.
func northwindSecurityPolicies() []securityPolicyDef {
	return []securityPolicyDef{
		{
			RID:           "ri.ontology.main.security-policy.employee-columns",
			ObjectTypeAPI: "employee",
			PolicyType:    "PROPERTY",
			// Baseline: everyone sees employeeId, name, department.
			// Editors (manager@test has "editor" global role) also see salary.
			RulesJSON: `[
				{"properties":["employeeId","name","department"]},
				{"userAttr":"roles","values":["editor","admin"],"properties":["salary"]}
			]`,
		},
	}
}

func northwindSchemas() []schema {
	return []schema{
		// FK-bearing properties (customerID) route through the
		// not_analyzed analyzer so TermQuery-based FK resolution in
		// pkg/links/fk_resolver.go matches "ALFKI" literally without
		// the English stemmer clobbering the value.
		{
			APIName:           "customer",
			DisplayName:       "Customer",
			PluralDisplayName: "Customers",
			PrimaryKey:        "customerID",
			TitleProperty:     "companyName",
			Properties: []schemaProperty{
				{APIName: "customerID", DisplayName: "Customer ID", BaseType: "string", IsSearchable: true, IsSortable: true, Analyzer: "not_analyzed"},
				{APIName: "companyName", DisplayName: "Company Name", BaseType: "string", IsSearchable: true, IsSortable: true},
				{APIName: "country", DisplayName: "Country", BaseType: "string", IsSearchable: true, IsSortable: true},
				{APIName: "contactName", DisplayName: "Contact Name", BaseType: "string", IsSearchable: true, IsSortable: false},
			},
			SeedRows: []map[string]interface{}{
				{"customerID": "ALFKI", "companyName": "Alfreds Futterkiste", "country": "germany", "contactName": "Maria Anders"},
				{"customerID": "BERGS", "companyName": "Berglunds snabbköp", "country": "sweden", "contactName": "Christina Berglund"},
				{"customerID": "BLONP", "companyName": "Blondesddsl père et fils", "country": "france", "contactName": "Frédérique Citeaux"},
				{"customerID": "CACTU", "companyName": "Cactus Comidas para llevar", "country": "argentina", "contactName": "Patricio Simpson"},
				{"customerID": "CHOPS", "companyName": "Chop-suey Chinese", "country": "switzerland", "contactName": "Yang Wang"},
			},
		},
		{
			APIName:           "order",
			DisplayName:       "Order",
			PluralDisplayName: "Orders",
			PrimaryKey:        "orderID",
			TitleProperty:     "orderID",
			Properties: []schemaProperty{
				{APIName: "orderID", DisplayName: "Order ID", BaseType: "string", IsSearchable: true, IsSortable: true, Analyzer: "not_analyzed"},
				{APIName: "customerID", DisplayName: "Customer ID", BaseType: "string", IsSearchable: true, IsSortable: true, Analyzer: "not_analyzed"},
				{APIName: "freight", DisplayName: "Freight", BaseType: "double", IsSearchable: false, IsSortable: true},
				{APIName: "shipCountry", DisplayName: "Ship Country", BaseType: "string", IsSearchable: true, IsSortable: true},
			},
			SeedRows: []map[string]interface{}{
				{"orderID": "10248", "customerID": "ALFKI", "freight": 32.38, "shipCountry": "germany"},
				{"orderID": "10249", "customerID": "ALFKI", "freight": 11.61, "shipCountry": "germany"},
				{"orderID": "10250", "customerID": "BERGS", "freight": 65.83, "shipCountry": "sweden"},
				{"orderID": "10251", "customerID": "BLONP", "freight": 41.34, "shipCountry": "france"},
				{"orderID": "10252", "customerID": "CHOPS", "freight": 51.30, "shipCountry": "switzerland"},
				{"orderID": "10253", "customerID": "CACTU", "freight": 58.17, "shipCountry": "argentina"},
			},
		},
		{
			APIName:           "product",
			DisplayName:       "Product",
			PluralDisplayName: "Products",
			PrimaryKey:        "productID",
			TitleProperty:     "productName",
			Properties: []schemaProperty{
				{APIName: "productID", DisplayName: "Product ID", BaseType: "string", IsSearchable: true, IsSortable: true},
				{APIName: "productName", DisplayName: "Product Name", BaseType: "string", IsSearchable: true, IsSortable: true},
				{APIName: "unitPrice", DisplayName: "Unit Price", BaseType: "double", IsSearchable: false, IsSortable: true},
				{APIName: "unitsInStock", DisplayName: "Units In Stock", BaseType: "integer", IsSearchable: false, IsSortable: true},
			},
			SeedRows: []map[string]interface{}{
				{"productID": "1", "productName": "Chai", "unitPrice": 18.0, "unitsInStock": 39},
				{"productID": "2", "productName": "Chang", "unitPrice": 19.0, "unitsInStock": 17},
				{"productID": "3", "productName": "Aniseed Syrup", "unitPrice": 10.0, "unitsInStock": 13},
				{"productID": "4", "productName": "Chef Anton's Cajun Seasoning", "unitPrice": 22.0, "unitsInStock": 53},
			},
		},
		// US-081: Employee object type with a salary property. The property-level
		// security policy seeded alongside this type (see northwindSecurityPolicies)
		// grants salary visibility only to editor/admin roles, so the Playwright
		// policy-column-hiding spec can verify per-role column masking end-to-end.
		{
			APIName:           "employee",
			DisplayName:       "Employee",
			PluralDisplayName: "Employees",
			PrimaryKey:        "employeeId",
			TitleProperty:     "name",
			Properties: []schemaProperty{
				{APIName: "employeeId", DisplayName: "Employee ID", BaseType: "string", IsSearchable: true, IsSortable: true, Analyzer: "not_analyzed"},
				{APIName: "name", DisplayName: "Name", BaseType: "string", IsSearchable: true, IsSortable: true},
				{APIName: "department", DisplayName: "Department", BaseType: "string", IsSearchable: true, IsSortable: true},
				{APIName: "salary", DisplayName: "Salary", BaseType: "double", IsSearchable: false, IsSortable: true},
			},
			SeedRows: []map[string]interface{}{
				{"employeeId": "emp1", "name": "Alice Chen", "department": "engineering", "salary": float64(120000)},
				{"employeeId": "emp2", "name": "Bob Smith", "department": "engineering", "salary": float64(95000)},
				{"employeeId": "emp3", "name": "Carol Davis", "department": "marketing", "salary": float64(110000)},
			},
		},
	}
}

// interfaceDef describes a Foundry-style Interface that spans multiple
// ObjectTypes. The Playwright interface-multitype-paging spec (US-041)
// drives loadObjectsOrInterfaces against the resulting object_type_interfaces
// rows, so the implementers list must match the schema() ObjectTypes
// above and carry enough total seed rows for a multi-page cursor walk.
type interfaceDef struct {
	APIName      string
	DisplayName  string
	Implementers []string // object type apiNames
}

// northwindInterfaces returns the baseline interface catalogue the Phase 6
// gate depends on. Only HasOwner is seeded today; extend this list if a
// future story needs a second polymorphic contract. Implementers must
// appear in northwindSchemas() above so the seed loop can resolve each
// ObjectType RID without reading back from PG.
func northwindInterfaces() []interfaceDef {
	return []interfaceDef{
		{
			APIName:      "HasOwner",
			DisplayName:  "Has Owner",
			Implementers: []string{"customer", "order", "product"},
		},
	}
}

func northwindLinkTypes() []linkDef {
	return []linkDef{
		{
			APIName:     "customerOrders",
			DisplayName: "Customer Orders",
			Source:      "customer",
			Target:      "order",
			Cardinality: "ONE_TO_MANY",
			// customer.customerID (PK) -> order.customerID (FK) — the
			// Playwright withProperties spec (US-040) depends on this
			// link being FK-resolvable end-to-end.
			FK: &fkConfigDef{SourceProperty: "customerID", TargetProperty: "customerID"},
		},
		{
			APIName:     "orderCustomer",
			DisplayName: "Order Customer",
			Source:      "order",
			Target:      "customer",
			Cardinality: "ONE_TO_ONE",
			FK:          &fkConfigDef{SourceProperty: "customerID", TargetProperty: "customerID"},
		},
	}
}

type actionTypeDef struct {
	APIName     string
	DisplayName string
	Description string
	Parameters  string // JSON array of actionParamDef
	Rules       string // JSON array of pkg/actions.Rule
}

// northwindActionTypes returns the baseline ActionTypes the Playwright E2E
// specs need to exercise the Action Console flows (US-038 optimistic
// concurrency in particular). Each definition is a minimal JSON snippet
// that lines up with pkg/actions.ParseRules / parametersToV2.
func northwindActionTypes() []actionTypeDef {
	return []actionTypeDef{
		{
			APIName:     "updateCustomerContact",
			DisplayName: "Update Customer Contact",
			Description: "Change the contact name on a customer record (used by US-038 optimistic-concurrency spec).",
			Parameters: `[
				{"id":"primaryKey","type":"string","required":true,"description":"Customer primary key"},
				{"id":"contactName","type":"string","required":true,"description":"New contact name"}
			]`,
			Rules: `[
				{
					"type":"modifyObject",
					"objectType":"customer",
					"propertyBindings":{
						"contactName":{"type":"parameter","value":"contactName"}
					}
				}
			]`,
		},
		{
			APIName:     "createCustomer",
			DisplayName: "Create Customer",
			Description: "Create a new customer record (used by US-079 browser-realtime-mode spec).",
			Parameters: `[
				{"id":"customerID","type":"string","required":true,"description":"Customer ID"},
				{"id":"companyName","type":"string","required":true,"description":"Company name"},
				{"id":"country","type":"string","required":false,"description":"Country"},
				{"id":"contactName","type":"string","required":false,"description":"Contact name"}
			]`,
			Rules: `[
				{
					"type":"createObject",
					"objectType":"customer",
					"propertyBindings":{
						"customerID":{"type":"parameter","value":"customerID"},
						"companyName":{"type":"parameter","value":"companyName"},
						"country":{"type":"parameter","value":"country"},
						"contactName":{"type":"parameter","value":"contactName"}
					}
				}
			]`,
		},
	}
}
