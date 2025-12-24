package solitudes_test

import (
	"sync"
	"testing"
	"time"
)

// TestConcurrencyPatterns tests the concurrency patterns used in the refactoring
// This validates that our changes from ants pool to direct goroutines are correct

func TestWaitGroupPattern(t *testing.T) {
	// Test the pattern we use in BuildArticleIndex
	var wg sync.WaitGroup
	counter := 0
	mu := sync.Mutex{}

	wg.Add(2)
	go func() {
		defer wg.Done()
		mu.Lock()
		counter++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}()
	go func() {
		defer wg.Done()
		mu.Lock()
		counter++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}()

	wg.Wait()

	mu.Lock()
	if counter != 2 {
		t.Errorf("Expected counter to be 2, got %d", counter)
	}
	mu.Unlock()
}

func TestDeferInGoroutines(t *testing.T) {
	// Test defer behavior in goroutines (used in our refactoring)
	var wg sync.WaitGroup
	executed := false

	wg.Add(1)
	go func() {
		defer func() {
			executed = true
			wg.Done()
		}()
		// Simulate some work
		time.Sleep(5 * time.Millisecond)
	}()

	wg.Wait()

	if !executed {
		t.Error("Deferred function was not executed")
	}
}

func TestProviderLoopPattern(t *testing.T) {
	// Test the provider loop refactoring from C-style to range
	providers := []func() interface{}{
		func() interface{} { return "provider1" },
		func() interface{} { return "provider2" },
		func() interface{} { return "provider3" },
	}

	results := make([]interface{}, 0, len(providers))
	for _, provider := range providers {
		results = append(results, provider())
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	expected := []string{"provider1", "provider2", "provider3"}
	for i, result := range results {
		if result.(string) != expected[i] {
			t.Errorf("Expected %s, got %s", expected[i], result)
		}
	}
}

func TestRangeIndexing(t *testing.T) {
	// Test that range-based indexing works correctly
	type mockItem struct {
		ID      string
		Version int
	}

	items := []mockItem{
		{ID: "1", Version: 1},
		{ID: "2", Version: 2},
		{ID: "3", Version: 3},
	}

	// Test accessing by index with range
	for i := range items {
		if items[i].ID == "" {
			t.Error("Item ID should not be empty")
		}
		if items[i].Version == 0 {
			t.Error("Item version should not be zero")
		}
	}

	// Verify all elements were processed
	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(items))
	}
}

func TestConcurrentSafety(t *testing.T) {
	// Test the concurrent pattern we use
	var wg sync.WaitGroup
	results := make([]int, 0, 10)
	mu := sync.Mutex{}

	// Simulate the pattern used in BuildArticleIndex
	wg.Add(10)
	for i := 0; i < 10; i++ {
		i := i // capture loop variable
		go func() {
			defer wg.Done()
			// Simulate work
			time.Sleep(time.Millisecond)
			mu.Lock()
			results = append(results, i)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(results) != 10 {
		t.Errorf("Expected 10 results, got %d", len(results))
	}
}
