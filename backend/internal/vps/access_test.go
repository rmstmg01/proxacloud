package vps

import "testing"

func TestValidateCustomerAccessMethod(t *testing.T) {
	if err := validateCustomerAccessMethod(0, ""); err == nil {
		t.Fatal("expected an error when neither customer access method is provided")
	}
	if err := validateCustomerAccessMethod(42, ""); err != nil {
		t.Fatalf("SSH-key-only access returned error: %v", err)
	}
	if err := validateCustomerAccessMethod(0, "ValidPassword1!"); err != nil {
		t.Fatalf("password-only access returned error: %v", err)
	}
	if err := validateCustomerAccessMethod(42, "ValidPassword1!"); err != nil {
		t.Fatalf("combined access returned error: %v", err)
	}
}
