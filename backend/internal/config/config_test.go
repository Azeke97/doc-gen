package config

import "testing"

func TestValidateDemo(t *testing.T) {
	tests := []struct {
		name string
		cfg  AdminConfig
		ok   bool
	}{
		{"off", AdminConfig{}, true},
		{"off with leftovers", AdminConfig{DemoEmail: "x@y.z"}, true},
		{"on", AdminConfig{DemoLogin: true, DemoEmail: "beta@docgen.test", DemoPassword: "long-enough-pw"}, true},
		{"no email", AdminConfig{DemoLogin: true, DemoPassword: "long-enough-pw"}, false},
		{"short password", AdminConfig{DemoLogin: true, DemoEmail: "beta@docgen.test", DemoPassword: "short"}, false},
	}
	for _, tt := range tests {
		if err := tt.cfg.validateDemo(); (err == nil) != tt.ok {
			t.Errorf("%s: err = %v", tt.name, err)
		}
	}
}
