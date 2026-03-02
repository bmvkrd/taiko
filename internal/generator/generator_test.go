package generator

import (
	"sync"
	"testing"
)

func TestNewIntRangeGenerator(t *testing.T) {
	tests := []struct {
		name    string
		min     int
		max     int
		mode    Mode
		wantErr bool
	}{
		{"valid range seq", 1, 10, SeqMode, false},
		{"valid range rnd", 1, 10, RndMode, false},
		{"single value", 5, 5, SeqMode, false},
		{"negative range", -10, -1, SeqMode, false},
		{"zero crossing", -5, 5, SeqMode, false},
		{"invalid min > max", 10, 1, SeqMode, true},
		{"invalid mode", 1, 10, "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewIntRangeGenerator(tt.min, tt.max, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIntRangeGenerator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && gen == nil {
				t.Error("NewIntRangeGenerator() returned nil generator without error")
			}
		})
	}
}

func TestIntRangeGenerator_Next_Seq(t *testing.T) {
	gen, err := NewIntRangeGenerator(1, 3, SeqMode)
	if err != nil {
		t.Fatalf("NewIntRangeGenerator() error = %v", err)
	}

	// Test sequential values
	expected := []int{1, 2, 3, 1, 2, 3, 1}
	for i, want := range expected {
		got := gen.Next().(int)
		if got != want {
			t.Errorf("Next() call %d = %v, want %v", i, got, want)
		}
	}
}

func TestIntRangeGenerator_Next_Rnd(t *testing.T) {
	gen, err := NewIntRangeGenerator(1, 100, RndMode)
	if err != nil {
		t.Fatalf("NewIntRangeGenerator() error = %v", err)
	}

	// Test that random values are within range and not all the same
	values := make(map[int]bool)
	for i := 0; i < 100; i++ {
		got := gen.Next().(int)
		if got < 1 || got > 100 {
			t.Errorf("Next() call %d = %v, out of range [1, 100]", i, got)
		}
		values[got] = true
	}

	// With 100 calls over a range of 100, we should see multiple distinct values
	if len(values) < 10 {
		t.Errorf("Random mode produced only %d distinct values in 100 calls, expected more variety", len(values))
	}
}

func TestIntRangeGenerator_Next_SingleValue(t *testing.T) {
	gen, err := NewIntRangeGenerator(42, 42, SeqMode)
	if err != nil {
		t.Fatalf("NewIntRangeGenerator() error = %v", err)
	}

	// Single value should always return the same value
	for i := 0; i < 5; i++ {
		got := gen.Next().(int)
		if got != 42 {
			t.Errorf("Next() call %d = %v, want 42", i, got)
		}
	}
}

func TestIntRangeGenerator_Next_NegativeRange(t *testing.T) {
	gen, err := NewIntRangeGenerator(-2, 1, SeqMode)
	if err != nil {
		t.Fatalf("NewIntRangeGenerator() error = %v", err)
	}

	expected := []int{-2, -1, 0, 1, -2, -1}
	for i, want := range expected {
		got := gen.Next().(int)
		if got != want {
			t.Errorf("Next() call %d = %v, want %v", i, got, want)
		}
	}
}

func TestIntRangeGenerator_GetGeneratorType(t *testing.T) {
	gen, _ := NewIntRangeGenerator(1, 10, SeqMode)
	if gen.GetGeneratorType() != IntRangeType {
		t.Errorf("GetGeneratorType() = %v, want %v", gen.GetGeneratorType(), IntRangeType)
	}
}

func TestIntRangeGenerator_Concurrent_Seq(t *testing.T) {
	gen, err := NewIntRangeGenerator(0, 99, SeqMode)
	if err != nil {
		t.Fatalf("NewIntRangeGenerator() error = %v", err)
	}

	const numGoroutines = 100
	const callsPerGoroutine = 1000

	var wg sync.WaitGroup
	results := make(chan int, numGoroutines*callsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				val := gen.Next().(int)
				results <- val
			}
		}()
	}

	wg.Wait()
	close(results)

	// Verify all values are in valid range
	counts := make(map[int]int)
	for val := range results {
		if val < 0 || val > 99 {
			t.Errorf("Got out of range value: %d", val)
		}
		counts[val]++
	}

	// Each value should appear approximately equal number of times
	// With 100,000 calls and 100 values, each should appear ~1000 times
	totalCalls := numGoroutines * callsPerGoroutine
	expectedPerValue := totalCalls / 100
	for val, count := range counts {
		// Allow 20% deviation
		if count < expectedPerValue*8/10 || count > expectedPerValue*12/10 {
			t.Errorf("Value %d appeared %d times, expected ~%d", val, count, expectedPerValue)
		}
	}
}

func TestIntRangeGenerator_Concurrent_Rnd(t *testing.T) {
	gen, err := NewIntRangeGenerator(0, 99, RndMode)
	if err != nil {
		t.Fatalf("NewIntRangeGenerator() error = %v", err)
	}

	const numGoroutines = 100
	const callsPerGoroutine = 1000

	var wg sync.WaitGroup
	results := make(chan int, numGoroutines*callsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				val := gen.Next().(int)
				results <- val
			}
		}()
	}

	wg.Wait()
	close(results)

	// Verify all values are in valid range
	for val := range results {
		if val < 0 || val > 99 {
			t.Errorf("Got out of range value: %d", val)
		}
	}
}

func TestNewIntSetGenerator(t *testing.T) {
	tests := []struct {
		name    string
		values  []int
		mode    Mode
		wantErr bool
	}{
		{"valid values seq", []int{1, 2, 3}, SeqMode, false},
		{"valid values rnd", []int{1, 2, 3}, RndMode, false},
		{"single value", []int{42}, SeqMode, false},
		{"negative values", []int{-5, -3, -1}, SeqMode, false},
		{"mixed values", []int{-10, 0, 10}, SeqMode, false},
		{"empty slice", []int{}, SeqMode, true},
		{"nil slice", nil, SeqMode, true},
		{"invalid mode", []int{1, 2, 3}, "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewIntSetGenerator(tt.values, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIntSetGenerator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && gen == nil {
				t.Error("NewIntSetGenerator() returned nil generator without error")
			}
		})
	}
}

func TestIntSetGenerator_Next_Seq(t *testing.T) {
	gen, err := NewIntSetGenerator([]int{10, 20, 30}, SeqMode)
	if err != nil {
		t.Fatalf("NewIntSetGenerator() error = %v", err)
	}

	// Test sequential values with wrap-around
	expected := []int{10, 20, 30, 10, 20, 30, 10}
	for i, want := range expected {
		got := gen.Next().(int)
		if got != want {
			t.Errorf("Next() call %d = %v, want %v", i, got, want)
		}
	}
}

func TestIntSetGenerator_Next_Rnd(t *testing.T) {
	values := []int{10, 20, 30, 40, 50}
	gen, err := NewIntSetGenerator(values, RndMode)
	if err != nil {
		t.Fatalf("NewIntSetGenerator() error = %v", err)
	}

	validValues := make(map[int]bool)
	for _, v := range values {
		validValues[v] = true
	}

	// Test that random values are from the set and not all the same
	seenValues := make(map[int]bool)
	for i := 0; i < 100; i++ {
		got := gen.Next().(int)
		if !validValues[got] {
			t.Errorf("Next() call %d = %v, not in set", i, got)
		}
		seenValues[got] = true
	}

	// With 100 calls over a set of 5, we should see multiple distinct values
	if len(seenValues) < 3 {
		t.Errorf("Random mode produced only %d distinct values in 100 calls, expected more variety", len(seenValues))
	}
}

func TestIntSetGenerator_Next_SingleValue(t *testing.T) {
	gen, err := NewIntSetGenerator([]int{99}, SeqMode)
	if err != nil {
		t.Fatalf("NewIntSetGenerator() error = %v", err)
	}

	for i := 0; i < 5; i++ {
		got := gen.Next().(int)
		if got != 99 {
			t.Errorf("Next() call %d = %v, want 99", i, got)
		}
	}
}

func TestIntSetGenerator_ImmutableValues(t *testing.T) {
	original := []int{1, 2, 3}
	gen, err := NewIntSetGenerator(original, SeqMode)
	if err != nil {
		t.Fatalf("NewIntSetGenerator() error = %v", err)
	}

	// Modify original slice
	original[0] = 999

	// Generator should still return original values
	got := gen.Next().(int)
	if got != 1 {
		t.Errorf("Next() = %v, want 1 (generator should have copied values)", got)
	}
}

func TestIntSetGenerator_GetGeneratorType(t *testing.T) {
	gen, _ := NewIntSetGenerator([]int{1, 2, 3}, SeqMode)
	if gen.GetGeneratorType() != IntSetType {
		t.Errorf("GetGeneratorType() = %v, want %v", gen.GetGeneratorType(), IntSetType)
	}
}

func TestIntSetGenerator_Concurrent_Seq(t *testing.T) {
	values := []int{1, 2, 3, 4, 5}
	gen, err := NewIntSetGenerator(values, SeqMode)
	if err != nil {
		t.Fatalf("NewIntSetGenerator() error = %v", err)
	}

	const numGoroutines = 100
	const callsPerGoroutine = 1000

	var wg sync.WaitGroup
	results := make(chan int, numGoroutines*callsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				val := gen.Next().(int)
				results <- val
			}
		}()
	}

	wg.Wait()
	close(results)

	// Verify all values are from the set
	validValues := make(map[int]bool)
	for _, v := range values {
		validValues[v] = true
	}

	counts := make(map[int]int)
	for val := range results {
		if !validValues[val] {
			t.Errorf("Got invalid value: %d", val)
		}
		counts[val]++
	}

	// Each value should appear approximately equal number of times
	totalCalls := numGoroutines * callsPerGoroutine
	expectedPerValue := totalCalls / len(values)
	for val, count := range counts {
		// Allow 20% deviation
		if count < expectedPerValue*8/10 || count > expectedPerValue*12/10 {
			t.Errorf("Value %d appeared %d times, expected ~%d", val, count, expectedPerValue)
		}
	}
}

func TestIntSetGenerator_Concurrent_Rnd(t *testing.T) {
	values := []int{1, 2, 3, 4, 5}
	gen, err := NewIntSetGenerator(values, RndMode)
	if err != nil {
		t.Fatalf("NewIntSetGenerator() error = %v", err)
	}

	const numGoroutines = 100
	const callsPerGoroutine = 1000

	var wg sync.WaitGroup
	results := make(chan int, numGoroutines*callsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				val := gen.Next().(int)
				results <- val
			}
		}()
	}

	wg.Wait()
	close(results)

	// Verify all values are from the set
	validValues := make(map[int]bool)
	for _, v := range values {
		validValues[v] = true
	}

	for val := range results {
		if !validValues[val] {
			t.Errorf("Got invalid value: %d", val)
		}
	}
}

func TestNewUUIDGenerator(t *testing.T) {
	gen := NewUUIDGenerator()
	if gen == nil {
		t.Error("NewUUIDGenerator() returned nil")
	}
}

func TestUUIDGenerator_Next_Format(t *testing.T) {
	gen := NewUUIDGenerator()

	for i := 0; i < 100; i++ {
		uuid := gen.Next().(string)

		// Check length (8-4-4-4-12 = 36 characters)
		if len(uuid) != 36 {
			t.Errorf("Next() call %d: UUID length = %d, want 36", i, len(uuid))
		}

		// Check format (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)
		if uuid[8] != '-' || uuid[13] != '-' || uuid[18] != '-' || uuid[23] != '-' {
			t.Errorf("Next() call %d: UUID format invalid: %s", i, uuid)
		}

		// Check version 4 indicator (character at position 14 should be '4')
		if uuid[14] != '4' {
			t.Errorf("Next() call %d: UUID version = %c, want '4'", i, uuid[14])
		}

		// Check variant (character at position 19 should be 8, 9, a, or b)
		variant := uuid[19]
		if variant != '8' && variant != '9' && variant != 'a' && variant != 'b' {
			t.Errorf("Next() call %d: UUID variant = %c, want 8/9/a/b", i, variant)
		}
	}
}

func TestUUIDGenerator_Next_Unique(t *testing.T) {
	gen := NewUUIDGenerator()

	seen := make(map[string]bool)
	const numCalls = 10000

	for i := 0; i < numCalls; i++ {
		uuid := gen.Next().(string)
		if seen[uuid] {
			t.Errorf("Duplicate UUID generated: %s", uuid)
		}
		seen[uuid] = true
	}

	if len(seen) != numCalls {
		t.Errorf("Expected %d unique UUIDs, got %d", numCalls, len(seen))
	}
}

func TestUUIDGenerator_GetGeneratorType(t *testing.T) {
	gen := NewUUIDGenerator()
	if gen.GetGeneratorType() != UUIDType {
		t.Errorf("GetGeneratorType() = %v, want %v", gen.GetGeneratorType(), UUIDType)
	}
}

func TestUUIDGenerator_Concurrent(t *testing.T) {
	gen := NewUUIDGenerator()

	const numGoroutines = 100
	const callsPerGoroutine = 1000

	var wg sync.WaitGroup
	results := make(chan string, numGoroutines*callsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				uuid := gen.Next().(string)
				results <- uuid
			}
		}()
	}

	wg.Wait()
	close(results)

	// Verify all UUIDs are unique and valid
	seen := make(map[string]bool)
	for uuid := range results {
		if len(uuid) != 36 {
			t.Errorf("Invalid UUID length: %d", len(uuid))
		}
		if seen[uuid] {
			t.Errorf("Duplicate UUID: %s", uuid)
		}
		seen[uuid] = true
	}

	expectedCount := numGoroutines * callsPerGoroutine
	if len(seen) != expectedCount {
		t.Errorf("Expected %d unique UUIDs, got %d", expectedCount, len(seen))
	}
}

// Test that generators implement the Generator interface
func TestGeneratorInterface(t *testing.T) {
	var _ Generator = (*IntRangeGenerator)(nil)
	var _ Generator = (*IntSetGenerator)(nil)
	var _ Generator = (*UUIDGenerator)(nil)
}
