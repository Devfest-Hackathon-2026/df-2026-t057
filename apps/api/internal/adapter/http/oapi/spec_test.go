package oapi

import "testing"

// 埋め込んだ仕様が読み込めることを確かめる（厳密な JSON の検査に使う）。
func TestEmbeddedSpecLoads(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Paths.Find("/tasks") == nil {
		t.Fatal("missing /tasks")
	}
	s := spec.Components.Schemas["TaskCreateInput"].Value
	if len(s.AllOf) != 2 {
		t.Fatalf("allOf = %d", len(s.AllOf))
	}
	n := spec.Components.Schemas["TaskList"].Value.Properties["nextSnooze"].Value
	t.Logf("nextSnooze oneOf=%d types=%v", len(n.OneOf), n.OneOf[1].Value.Type)
}
