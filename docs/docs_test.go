package docs

import "testing"

func TestSwaggerInfo(t *testing.T) {
	if SwaggerInfo == nil {
		t.Fatal("SwaggerInfo is nil")
	}
	if SwaggerInfo.InstanceName() != "swagger" {
		t.Fatalf("InstanceName() = %q, want %q", SwaggerInfo.InstanceName(), "swagger")
	}
	if SwaggerInfo.Title != "Kestrix API" {
		t.Fatalf("Title = %q, want %q", SwaggerInfo.Title, "Kestrix API")
	}
}
