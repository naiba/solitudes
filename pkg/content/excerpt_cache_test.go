package content

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestExcerptLRUEvictionAndConcurrency(t *testing.T) {
	c := excerptLRU{entries: make(map[excerptKey]*list.Element)}
	key := func(i int) excerptKey { return excerptKey{Hash: sha256.Sum256([]byte(fmt.Sprint(i))), Limit: 100} }
	for i := 0; i < excerptCacheCapacity; i++ {
		c.put(key(i), fmt.Sprint(i))
	}
	if got, ok := c.get(key(0)); !ok || got != "0" {
		t.Fatal("cache miss", got)
	}
	c.put(key(excerptCacheCapacity), "new")
	if _, ok := c.get(key(1)); ok {
		t.Fatal("least recently used entry was retained")
	}
	if _, ok := c.get(key(0)); !ok {
		t.Fatal("recent entry was evicted")
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 512; i++ {
				c.put(key(worker*512+i), "text")
				c.get(key(i))
			}
		}(worker)
	}
	wg.Wait()
	if len(c.entries) != excerptCacheCapacity || c.order.Len() != excerptCacheCapacity {
		t.Fatal("unbounded cache")
	}
}

func TestExcerptCacheVariesWithContentAndLimit(t *testing.T) {
	for _, raw := range []string{"Hello **world**", "Edited **world**", "中文摘要", "Visible\n\n```access:members\nsecret\n```"} {
		for _, limit := range []int{0, 2, 20, 512, 513} {
			want := excerpt(raw, limit)
			if limit <= 0 {
				want = ""
			}
			for i := 0; i < 2; i++ {
				if got := Excerpt(raw, limit); got != want || strings.Contains(got, "secret") {
					t.Fatalf("limit %d: got %q want %q", limit, got, want)
				}
			}
		}
	}
}

func BenchmarkExcerpt(b *testing.B) {
	raw := strings.Repeat("A paragraph with **emphasis**, [links](https://example.test), and 中文.\n\n", 100)
	for _, warm := range []bool{false, true} {
		b.Run(fmt.Sprintf("cached=%t", warm), func(b *testing.B) {
			Excerpt(raw, 150)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if warm {
					Excerpt(raw, 150)
				} else {
					excerpt(raw, 150)
				}
			}
		})
	}
}
