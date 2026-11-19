package support

import (
	"encoding/json"
	"testing"
)

func TestSetNestedFoldsTwoFieldsIntoOneObject(t *testing.T) {
	params := map[string]json.RawMessage{}
	SetNested(params, "thinking.budget_tokens", 2048)
	SetNested(params, "thinking.type", "adaptive")
	thinking, ok := params["thinking"]
	if !ok {
		t.Fatal("the thinking parameter was not set")
	}
	var object map[string]any
	if err := json.Unmarshal(thinking, &object); err != nil {
		t.Fatalf("the merged parameter is not one object: %v", err)
	}
	if object["type"] != "adaptive" || object["budget_tokens"] != float64(2048) {
		t.Errorf("the merged object is %v", object)
	}
}

func TestSetNestedKeepsThePartsAnotherCallSet(t *testing.T) {
	params := map[string]json.RawMessage{}
	SetNested(params, "output_config.effort", "high")
	SetNested(params, "output_config.reasoning", "adaptive")
	config, ok := params["output_config"]
	if !ok {
		t.Fatal("the output config was not set")
	}
	var object map[string]any
	if err := json.Unmarshal(config, &object); err != nil {
		t.Fatalf("the merged parameter is not one object: %v", err)
	}
	if object["effort"] != "high" || object["reasoning"] != "adaptive" {
		t.Errorf("the second field replaced the first: %v", object)
	}
}

func TestSetNestedReplacesASingleField(t *testing.T) {
	params := map[string]json.RawMessage{}
	SetNested(params, "temperature", 0.5)
	SetNested(params, "temperature", 0.7)
	if string(params["temperature"]) != "0.7" {
		t.Errorf("the parameter is %s, want the second write", params["temperature"])
	}
}

func TestMergeParamsWritesTheSameBodyEveryTime(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":16}`)
	params := map[string]json.RawMessage{
		"output_config": json.RawMessage(`{"effort":"high"}`),
		"thinking":      json.RawMessage(`{"type":"adaptive"}`),
	}
	merged, err := MergeParams(body, params)
	if err != nil {
		t.Fatalf("the merge: %v", err)
	}
	// The parameters are written last, in name order, so the same request
	// is the same bytes.
	want := `{"model":"m","max_tokens":16,"output_config":{"effort":"high"},"thinking":{"type":"adaptive"}}`
	if string(merged) != want {
		t.Errorf("the merged body is %s, want %s", merged, want)
	}
	again, err := MergeParams(body, params)
	if err != nil {
		t.Fatalf("the merge: %v", err)
	}
	if string(again) != string(merged) {
		t.Errorf("the merge is not stable: %s then %s", merged, again)
	}
	plain, err := MergeParams(body, nil)
	if err != nil {
		t.Fatalf("the merge: %v", err)
	}
	if string(plain) != string(body) {
		t.Errorf("a request with no parameters is %s, want the body as it was", plain)
	}
}
