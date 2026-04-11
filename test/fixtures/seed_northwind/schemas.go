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
}

type linkDef struct {
	APIName     string
	DisplayName string
	Source      string // object type apiName
	Target      string // object type apiName
	Cardinality string // ONE_TO_MANY / MANY_TO_MANY / ...
}

func northwindSchemas() []schema {
	return []schema{
		{
			APIName:           "customer",
			DisplayName:       "Customer",
			PluralDisplayName: "Customers",
			PrimaryKey:        "customerID",
			TitleProperty:     "companyName",
			Properties: []schemaProperty{
				{APIName: "customerID", DisplayName: "Customer ID", BaseType: "string", IsSearchable: true, IsSortable: true},
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
				{APIName: "orderID", DisplayName: "Order ID", BaseType: "string", IsSearchable: true, IsSortable: true},
				{APIName: "customerID", DisplayName: "Customer ID", BaseType: "string", IsSearchable: true, IsSortable: true},
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
		},
		{
			APIName:     "orderCustomer",
			DisplayName: "Order Customer",
			Source:      "order",
			Target:      "customer",
			Cardinality: "ONE_TO_ONE",
		},
	}
}
