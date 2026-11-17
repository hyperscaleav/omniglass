package erd

import "testing"

// TestRenderClustersAndEdges pins the D2 emission: named containers in cluster
// order, each table a sql_table node listing only its primary-key and
// foreign-key columns, foreign keys as cross-container edges, and a table absent
// from every cluster placed in a trailing "unclustered" container so it is
// visible rather than dropped.
func TestRenderClustersAndEdges(t *testing.T) {
	s := Schema{Tables: []Table{
		{
			Name:    "principal",
			Columns: []Column{{Name: "id", Type: "uuid", PK: true}},
		},
		{
			Name: "property",
			Columns: []Column{
				{Name: "id", Type: "uuid", PK: true},
				{Name: "property_type_id", Type: "uuid", FK: true},
			},
			FKs: []ForeignKey{{Column: "property_type_id", RefTable: "property_type", RefColumn: "id"}},
		},
		{
			Name:    "property_type",
			Columns: []Column{{Name: "id", Type: "uuid", PK: true}},
		},
	}}
	clusters := []Cluster{
		{Name: "identity", Tables: []string{"principal"}},
		{Name: "telemetry", Tables: []string{"property"}},
	}

	got, err := Render(s, clusters)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	want := `direction: right

identity: {
  principal: {
    shape: sql_table
    id: uuid {constraint: primary_key}
  }
}

telemetry: {
  property: {
    shape: sql_table
    id: uuid {constraint: primary_key}
    property_type_id: uuid {constraint: foreign_key}
  }
}

unclustered: {
  property_type: {
    shape: sql_table
    id: uuid {constraint: primary_key}
  }
}

telemetry.property.property_type_id -> unclustered.property_type.id
`
	if got != want {
		t.Errorf("Render mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
