package main

import "testing"

func TestDatabasePoolBudgets(t *testing.T) {
	api, worker, control := databasePoolBudgets("all", 64)
	if api != 24 || worker != 32 || control != 8 || api+worker+control != 64 {
		t.Fatalf("all budgets=%d/%d/%d", api, worker, control)
	}
	api, worker, control = databasePoolBudgets("worker", 64)
	if api != 0 || worker != 48 || control != 16 {
		t.Fatalf("worker budgets=%d/%d/%d", api, worker, control)
	}
	api, worker, control = databasePoolBudgets("api", 64)
	if api != 64 || worker != 0 || control != 0 {
		t.Fatalf("api budgets=%d/%d/%d", api, worker, control)
	}
}
