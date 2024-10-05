package ptr

import (
	"reflect"
	"testing"
)

func TestToPtr(t *testing.T) {
	t.Run("ReturnsPointerToGivenValue", func(t *testing.T) {
		value := 5
		ptr := ToPtr(value)
		if *ptr != value {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsDifferentPointersForSameValue", func(t *testing.T) {
		value := 5
		ptr1 := ToPtr(value)
		ptr2 := ToPtr(value)
		if ptr1 == ptr2 {
			t.Errorf("Expected different pointers for same value")
		}
	})
	t.Run("ReturnsPointerToZeroValue", func(t *testing.T) {
		var value int
		ptr := ToPtr(value)
		if *ptr != value {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsPointerToStruct", func(t *testing.T) {
		type TestStruct struct {
			Name string
		}
		value := TestStruct{Name: "test"}
		ptr := ToPtr(value)
		if *ptr != value {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsPointerToSlice", func(t *testing.T) {
		value := []int{1, 2, 3}
		ptr := ToPtr(value)
		if !reflect.DeepEqual(*ptr, value) {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsPointerToMap", func(t *testing.T) {
		value := map[string]int{"one": 1, "two": 2}
		ptr := ToPtr(value)
		if !reflect.DeepEqual(*ptr, value) {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsPointerToBool", func(t *testing.T) {
		value := true
		ptr := ToPtr(value)
		if *ptr != value {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsPointerToString", func(t *testing.T) {
		value := "test"
		ptr := ToPtr(value)
		if *ptr != value {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsPointerToFloat", func(t *testing.T) {
		value := 3.14
		ptr := ToPtr(value)
		if *ptr != value {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsPointerToInterface", func(t *testing.T) {
		value := interface{}("test")
		ptr := ToPtr(value)
		if *ptr != value {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
	t.Run("ReturnsPointerToChannel", func(t *testing.T) {
		value := make(chan int)
		ptr := ToPtr(value)
		if *ptr != value {
			t.Errorf("Expected %v, got %v", value, *ptr)
		}
	})
}
