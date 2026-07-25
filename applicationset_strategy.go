package main

import (
	"errors"
	"fmt"

	"github.com/goccy/go-yaml"
)

type rollingSyncStrategy struct {
	steps []rollingSyncStep
}

type rollingSyncStep struct {
	expressions []labelExpression
}

func parseApplicationSetStrategy(items yaml.MapSlice) (*rollingSyncStrategy, error) {
	if items == nil {
		return nil, nil
	}
	strategyType := "AllAtOnce"
	var deletionOrder string
	var rollingSync any
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return nil, errors.New("spec.strategy contains a non-string field name")
		}
		switch key {
		case "type":
			value, ok := item.Value.(string)
			if !ok {
				return nil, errors.New("spec.strategy.type must be a string")
			}
			strategyType = value
		case "deletionOrder":
			value, ok := item.Value.(string)
			if !ok {
				return nil, errors.New("spec.strategy.deletionOrder must be a string")
			}
			deletionOrder = value
		case "rollingSync":
			rollingSync = item.Value
		default:
			return nil, fmt.Errorf("spec.strategy.%s is not supported", key)
		}
	}
	switch strategyType {
	case "AllAtOnce":
		return nil, nil
	case "RollingSync":
	default:
		return nil, fmt.Errorf(
			"spec.strategy.type must be AllAtOnce or RollingSync, got %q",
			strategyType,
		)
	}
	if deletionOrder != "Reverse" {
		return nil, errors.New("spec.strategy.deletionOrder must be Reverse for RollingSync")
	}
	options, ok := rollingSync.(yaml.MapSlice)
	if !ok {
		return nil, errors.New("spec.strategy.rollingSync must be a mapping")
	}
	return parseRollingSyncStrategy(options)
}

func parseRollingSyncStrategy(items yaml.MapSlice) (*rollingSyncStrategy, error) {
	var rawSteps []any
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return nil, errors.New("spec.strategy.rollingSync contains a non-string field name")
		}
		if key != "steps" {
			return nil, fmt.Errorf("spec.strategy.rollingSync.%s is not supported", key)
		}
		var stepsOK bool
		rawSteps, stepsOK = item.Value.([]any)
		if !stepsOK {
			return nil, errors.New("spec.strategy.rollingSync.steps must be a sequence")
		}
	}
	if len(rawSteps) == 0 {
		return nil, errors.New("spec.strategy.rollingSync.steps must contain at least one step")
	}
	result := &rollingSyncStrategy{steps: make([]rollingSyncStep, 0, len(rawSteps))}
	for i, rawStep := range rawSteps {
		field := fmt.Sprintf("spec.strategy.rollingSync.steps[%d]", i)
		items, ok := rawStep.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("%s must be a mapping", field)
		}
		step, err := parseRollingSyncStep(items, field)
		if err != nil {
			return nil, err
		}
		result.steps = append(result.steps, step)
	}
	return result, nil
}

func parseRollingSyncStep(items yaml.MapSlice, field string) (rollingSyncStep, error) {
	var result rollingSyncStep
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "matchExpressions":
			rawExpressions, ok := item.Value.([]any)
			if !ok {
				return result, fmt.Errorf("%s.matchExpressions must be a sequence", field)
			}
			for i, rawExpression := range rawExpressions {
				expressionField := fmt.Sprintf("%s.matchExpressions[%d]", field, i)
				expressionItems, ok := rawExpression.(yaml.MapSlice)
				if !ok {
					return result, fmt.Errorf("%s must be a mapping", expressionField)
				}
				expression, err := parseLabelExpression(expressionItems, expressionField)
				if err != nil {
					return result, err
				}
				if expression.operator != "In" && expression.operator != "NotIn" {
					return result, fmt.Errorf(
						"%s.operator must be In or NotIn",
						expressionField,
					)
				}
				result.expressions = append(result.expressions, expression)
			}
		case "maxUpdate":
			value, ok := item.Value.(string)
			if !ok || value != "100%" {
				return result, fmt.Errorf(
					"%s.maxUpdate must be the string %q when specified",
					field,
					"100%",
				)
			}
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	return result, nil
}

func assignRollingSyncSteps(
	applications []generatedApplication,
	strategy *rollingSyncStrategy,
) error {
	for i := range applications {
		labels := applications[i].application.Metadata.Labels
		var matches []int
		for stepIndex, step := range strategy.steps {
			selector := labelSelector{matchExpressions: step.expressions}
			if selector.matchesFlat(labels) {
				matches = append(matches, stepIndex)
			}
		}
		if len(matches) != 1 {
			return fmt.Errorf(
				"%s: generated Application %q must match exactly one RollingSync step; matched %d",
				applications[i].path,
				applications[i].application.Metadata.Name,
				len(matches),
			)
		}
		applications[i].rollingStep = &matches[0]
	}
	return nil
}
