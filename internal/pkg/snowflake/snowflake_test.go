package snowflake

import "testing"

func TestInit_Success(t *testing.T) {
	err := Init(1)
	if err != nil {
		t.Fatalf("Init(1) failed: %v", err)
	}
}

func TestGenID_Unique(t *testing.T) {
	_ = Init(1)

	ids := make(map[int64]bool)
	count := 1000
	for i := 0; i < count; i++ {
		id := GenID()
		if id == 0 {
			t.Fatal("GenID returned 0")
		}
		if ids[id] {
			t.Fatalf("GenID returned duplicate id: %d", id)
		}
		ids[id] = true
	}
}

func TestGenID_Positive(t *testing.T) {
	_ = Init(1)

	for i := 0; i < 100; i++ {
		id := GenID()
		if id <= 0 {
			t.Fatalf("GenID returned non-positive id: %d", id)
		}
	}
}
