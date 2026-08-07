package config

import (
	"reflect"
	"testing"
)

func TestResolvedTriggerTypesCarryOnlyTheDeclaredPrivatePositions(t *testing.T) {
	tests := []struct {
		value any
		want  map[string]string
	}{
		{
			value: TriggerSpec{},
			want:  map[string]string{"tablePosition": "config.Positioned"},
		},
		{
			value: Operation{},
			want: map[string]string{
				"columnsPosition": "config.Positioned",
				"whenPosition":    "config.Positioned",
			},
		},
	}

	for _, tc := range tests {
		typ := reflect.TypeOf(tc.value)
		t.Run(typ.Name(), func(t *testing.T) {
			got := make(map[string]string)
			for i := range typ.NumField() {
				field := typ.Field(i)
				if !field.IsExported() {
					got[field.Name] = field.Type.String()
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("private metadata = %v, want %v", got, tc.want)
			}
		})
	}
}
