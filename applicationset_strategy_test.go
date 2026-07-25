package main

import (
	"strings"
	"testing"
)

func TestConvertApplicationSetRollingSync(t *testing.T) {
	input := readTestdata(t, "applicationset/rolling-sync/application.yaml")
	config := readTestdata(t, "applicationset/rolling-sync/config.yaml")
	parsedConfig, err := parseConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	output, err := convertWithConfig([]byte(input), parsedConfig)
	if err != nil {
		t.Fatal(err)
	}
	want := readTestdata(t, "applicationset/rolling-sync/helmfile.yaml")
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func TestConvertApplicationSetAllAtOnceDoesNotAddNeeds(t *testing.T) {
	input := readTestdata(t, "applicationset/rolling-sync/all-at-once.yaml")
	withStrategy, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	withoutStrategyInput := strings.Replace(
		input,
		"  strategy:\n    type: AllAtOnce\n",
		"",
		1,
	)
	withoutStrategy, err := convert([]byte(withoutStrategyInput))
	if err != nil {
		t.Fatal(err)
	}
	if string(withStrategy) != string(withoutStrategy) {
		t.Fatalf(
			"AllAtOnce changed output:\n%s\nwithout strategy:\n%s",
			withStrategy,
			withoutStrategy,
		)
	}
	if strings.Contains(string(withStrategy), "needs:") {
		t.Fatalf("AllAtOnce emitted needs:\n%s", withStrategy)
	}
}

func TestConvertApplicationSetRollingSyncDoesNotCrossResourceBoundaries(t *testing.T) {
	input := readTestdata(t, "applicationset/rolling-sync/boundaries.yaml")
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{
		"  - name: first-app\n    chart: charts/service\n    version: 1.0.0\n" +
			"    needs:\n      - first-base\n",
		"  - name: direct\n    chart: charts/service\n    version: 1.0.0\n",
		"  - name: second-app\n    chart: charts/service\n    version: 1.0.0\n" +
			"    needs:\n      - second-base\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{
		"needs:\n      - direct",
		"needs:\n      - first-app",
		"needs:\n      - first-base\n      - second-base",
	} {
		if strings.Contains(text, unwanted) {
			t.Errorf("output unexpectedly contains %q:\n%s", unwanted, output)
		}
	}
}

func TestConvertApplicationSetRollingSyncRejectsUnsupportedMaxUpdate(t *testing.T) {
	valid := readTestdata(t, "applicationset/rolling-sync/application.yaml")
	tests := []struct {
		name        string
		replacement string
	}{
		{name: "integer 100", replacement: "100"},
		{name: "integer zero", replacement: "0"},
		{name: "partial percentage", replacement: "10%"},
		{name: "other string", replacement: `"all"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := strings.Replace(valid, `"100%"`, test.replacement, 1)
			_, err := convert([]byte(input))
			if err == nil || !strings.Contains(
				err.Error(),
				`spec.strategy.rollingSync.steps[0].maxUpdate must be the string "100%"`,
			) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConvertApplicationSetRollingSyncRequiresReverseDeletion(t *testing.T) {
	valid := readTestdata(t, "applicationset/rolling-sync/application.yaml")
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "omitted",
			input: strings.Replace(
				valid,
				"    deletionOrder: Reverse\n",
				"",
				1,
			),
		},
		{
			name:  "AllAtOnce",
			input: strings.Replace(valid, "deletionOrder: Reverse", "deletionOrder: AllAtOnce", 1),
		},
		{
			name:  "unknown",
			input: strings.Replace(valid, "deletionOrder: Reverse", "deletionOrder: Later", 1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := convert([]byte(test.input))
			if err == nil || !strings.Contains(
				err.Error(),
				"spec.strategy.deletionOrder must be Reverse for RollingSync",
			) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConvertApplicationSetRollingSyncErrors(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		wants   []string
	}{
		{
			name:    "unmatched Application",
			fixture: "unmatched.yaml",
			wants: []string{
				"document 1: spec.generators[0].list.elements[0]",
				`generated Application "orphan"`,
				"matched 0",
			},
		},
		{
			name:    "multiple matching steps",
			fixture: "multiple.yaml",
			wants: []string{
				"document 1: spec.generators[0].list.elements[0]",
				`generated Application "duplicate"`,
				"matched 2",
			},
		},
		{
			name:    "unsupported operator",
			fixture: "operator.yaml",
			wants: []string{
				"document 1",
				"spec.strategy.rollingSync.steps[0].matchExpressions[0].operator",
				"must be In or NotIn",
			},
		},
		{
			name:    "empty steps",
			fixture: "empty-steps.yaml",
			wants: []string{
				"document 1",
				"spec.strategy.rollingSync.steps must contain at least one step",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := readTestdata(
				t,
				"applicationset/rolling-sync/errors/"+test.fixture,
			)
			_, err := convert([]byte(input))
			if err == nil {
				t.Fatal("conversion unexpectedly succeeded")
			}
			for _, want := range test.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestConvertApplicationSetRejectsUnknownStrategyType(t *testing.T) {
	input := readTestdata(t, "applicationset/rolling-sync/application.yaml")
	input = strings.Replace(input, "type: RollingSync", "type: BlueGreen", 1)
	_, err := convert([]byte(input))
	if err == nil || !strings.Contains(
		err.Error(),
		`spec.strategy.type must be AllAtOnce or RollingSync, got "BlueGreen"`,
	) {
		t.Fatalf("unexpected error: %v", err)
	}
}
