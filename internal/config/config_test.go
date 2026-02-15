package config

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  LNDConfig
		ok   bool
	}{
		{"valid", LNDConfig{Host: "localhost:10009", TLSCertPath: "tls.cert", MacaroonPath: "admin.macaroon"}, true},
		{"missing host", LNDConfig{TLSCertPath: "tls.cert", MacaroonPath: "admin.macaroon"}, false},
		{"missing tls", LNDConfig{Host: "localhost:10009", MacaroonPath: "admin.macaroon"}, false},
		{"missing macaroon", LNDConfig{Host: "localhost:10009", TLSCertPath: "tls.cert"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.ok && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}
