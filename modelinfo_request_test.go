package poo_test

import (
	"errors"
	"testing"

	"github.com/sdougbrown/poo"
)

func TestMaxTokensUsesTheModelCeiling(t *testing.T) {
	info := poo.ModelInfo{MaxOutput: 4096, ContextLimit: 32768}
	if got := poo.MaxTokens(info, 1000); got != 4096 {
		t.Errorf("the output room is %d, want 4096", got)
	}
}

func TestMaxTokensFallsBackToTheDefaultCeiling(t *testing.T) {
	info := poo.ModelInfo{ContextLimit: 32768}
	if got := poo.MaxTokens(info, 1000); got != poo.DefaultMaxOutput {
		t.Errorf("the output room is %d, want %d", got, poo.DefaultMaxOutput)
	}
}

func TestMaxTokensIsCappedByTheRemainingWindow(t *testing.T) {
	info := poo.ModelInfo{MaxOutput: 8192, ContextLimit: 4096}
	want := 4096 - 1000 - poo.OutputReserve
	if got := poo.MaxTokens(info, 1000); got != want {
		t.Errorf("the output room is %d, want %d", got, want)
	}
}

func TestMaxTokensIgnoresAnUnknownWindow(t *testing.T) {
	info := poo.ModelInfo{MaxOutput: 2048}
	if got := poo.MaxTokens(info, 1000); got != 2048 {
		t.Errorf("the output room is %d, want 2048", got)
	}
}

func TestMaxTokensMatchesTheSessionRule(t *testing.T) {
	info := poo.ModelInfo{MaxOutput: 3000, ContextLimit: 8192}
	if got, want := info.MaxTokens(500), poo.MaxTokens(info, 500); got != want {
		t.Errorf("the method gives %d and the function gives %d", got, want)
	}
}

func TestEffectiveSamplingOverridesFieldByField(t *testing.T) {
	model := poo.ModelInfo{Sampling: poo.Sampling{
		Temperature: ptr(0.7),
		TopP:        ptr(0.95),
	}}
	override := poo.Sampling{Temperature: ptr(0.2)}
	got := model.EffectiveSampling(override)
	if got.Temperature == nil || *got.Temperature != 0.2 {
		t.Errorf("the temperature is %v, want 0.2", got.Temperature)
	}
	if got.TopP == nil || *got.TopP != 0.95 {
		t.Errorf("the top_p is %v, want the model's 0.95", got.TopP)
	}
}

func TestEffectiveSamplingDropsTheTemperatureOfAFixedModel(t *testing.T) {
	for _, mode := range []poo.TempMode{poo.TempFixed, poo.TempUnsupported} {
		model := poo.ModelInfo{
			Temperature: mode,
			Sampling:    poo.Sampling{Temperature: ptr(0.7)},
		}
		got := model.EffectiveSampling(poo.Sampling{Temperature: ptr(0.2)})
		if got.Temperature != nil {
			t.Errorf("the %s model sends the temperature %v", mode, *got.Temperature)
		}
	}
}

func TestEffectiveSamplingKeepsAFreeModelTemperature(t *testing.T) {
	model := poo.ModelInfo{Temperature: poo.TempFree, Sampling: poo.Sampling{Temperature: ptr(0.7)}}
	got := model.EffectiveSampling(poo.Sampling{})
	if got.Temperature == nil || *got.Temperature != 0.7 {
		t.Errorf("the temperature is %v, want the model's 0.7", got.Temperature)
	}
}

func TestReasoningFieldsMapTheSettingOntoTheRequest(t *testing.T) {
	tests := []struct {
		name    string
		model   poo.ModelInfo
		setting string
		want    []poo.ReasoningField
	}{
		{
			name:    "a toggle model is switched on",
			model:   poo.ModelInfo{Name: "gemma4", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningToggle}},
			setting: "low",
			want:    []poo.ReasoningField{{Path: poo.ReasoningPathToggle, Value: true}},
		},
		{
			name:    "a toggle model is switched off",
			model:   poo.ModelInfo{Name: "gemma4", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningToggle}},
			setting: "off",
			want:    []poo.ReasoningField{{Path: poo.ReasoningPathToggle, Value: false}},
		},
		{
			name:    "an effort model takes the level",
			model:   poo.ModelInfo{Name: "gpt-5", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningEffort}},
			setting: "high",
			want:    []poo.ReasoningField{{Path: poo.ReasoningPathEffort, Value: "high"}},
		},
		{
			name:    "a budget model takes the budget",
			model:   poo.ModelInfo{Name: "claude", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningBudget}},
			setting: "2048",
			want:    []poo.ReasoningField{{Path: poo.ReasoningPathBudget, Value: 2048}},
		},
		{
			name:    "an off setting asks for no field",
			model:   poo.ModelInfo{Name: "gpt-5", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningEffort}},
			setting: "off",
			want:    nil,
		},
		{
			name:    "a model without a control asks for no field",
			model:   poo.ModelInfo{Name: "plain"},
			setting: "off",
			want:    nil,
		},
		{
			name:    "the model spells the field itself",
			model:   poo.ModelInfo{Name: "gpt-5", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningEffort, Param: "reasoning.effort"}},
			setting: "medium",
			want:    []poo.ReasoningField{{Path: "reasoning.effort", Value: "medium"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.model.ReasoningFields(tc.setting)
			if err != nil {
				t.Fatalf("the fields: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("the fields are %v, want %v", got, tc.want)
			}
			for i, field := range got {
				if field != tc.want[i] {
					t.Errorf("the field is %+v, want %+v", field, tc.want[i])
				}
			}
		})
	}
}

func TestReasoningFieldsRejectAnImpossibleSetting(t *testing.T) {
	tests := []struct {
		name    string
		model   poo.ModelInfo
		setting string
	}{
		{
			name:    "a model without a control",
			model:   poo.ModelInfo{Name: "plain", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningNone}},
			setting: "high",
		},
		{
			name:    "an effort model with a budget",
			model:   poo.ModelInfo{Name: "gpt-5", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningEffort}},
			setting: "2048",
		},
		{
			name:    "a budget model with an effort",
			model:   poo.ModelInfo{Name: "claude", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningBudget}},
			setting: "low",
		},
		{
			name:    "a setting that is not one",
			model:   poo.ModelInfo{Name: "gpt-5", Reasoning: poo.ReasoningControl{Mode: poo.ReasoningEffort}},
			setting: "some",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.model.ReasoningFields(tc.setting)
			if err == nil {
				t.Fatal("the setting was accepted")
			}
			var cfg *poo.ConfigError
			if !errors.As(err, &cfg) {
				t.Fatalf("the error is %v, want a config error", err)
			}
			if cfg.Field != "reasoning.chat" {
				t.Errorf("the error names %q, want reasoning.chat", cfg.Field)
			}
		})
	}
}

func TestParseReasoning(t *testing.T) {
	setting, err := poo.ParseReasoning("Medium")
	if err != nil {
		t.Fatalf("the setting: %v", err)
	}
	if setting.Effort != "medium" || setting.Off {
		t.Errorf("the setting is %+v, want the medium effort", setting)
	}
	setting, err = poo.ParseReasoning("")
	if err != nil {
		t.Fatalf("the setting: %v", err)
	}
	if !setting.Off {
		t.Errorf("the setting is %+v, want off", setting)
	}
	if _, err := poo.ParseReasoning("-5"); err == nil {
		t.Error("a negative budget was accepted")
	}
}

// ptr returns a pointer to a float64.
func ptr(f float64) *float64 { return &f }
