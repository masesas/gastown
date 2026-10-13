package testutil

import "testing"

func TestStoreUnavailableIsFailure_FollowsContainer(t *testing.T) {
	if DoltContainerPort() == "" && storeUnavailableIsFailure() {
		t.Fatal("without a test container, an unavailable store must skip, not fail")
	}
	RequireDoltContainer(t) // skips when Docker is unavailable
	if !storeUnavailableIsFailure() {
		t.Fatal("with a running test container, an unavailable store must fail the test")
	}
}
