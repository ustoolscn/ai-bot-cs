package app

import "testing"

func TestValidateRuntimeSettings(t *testing.T) {
	valid := RuntimeSettings{DefaultContextLimit: 20, AIRequestTimeoutSeconds: 90, MessageRetentionDays: 90}
	if err := validateRuntimeSettings(valid); err != nil {
		t.Fatal(err)
	}
	tests := []RuntimeSettings{{DefaultContextLimit: 0, AIRequestTimeoutSeconds: 90, MessageRetentionDays: 90}, {DefaultContextLimit: 20, AIRequestTimeoutSeconds: 601, MessageRetentionDays: 90}, {DefaultContextLimit: 20, AIRequestTimeoutSeconds: 90, MessageRetentionDays: 0}}
	for _, test := range tests {
		if err := validateRuntimeSettings(test); err == nil {
			t.Fatalf("accepted invalid settings: %+v", test)
		}
	}
}
