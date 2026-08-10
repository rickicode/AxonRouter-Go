package quota

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkExhaustion_IsExhausted(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			ec := NewExhaustionCache()
			keys := make([]string, size)
			for i := 0; i < size; i++ {
				keys[i] = fmt.Sprintf("conn-%d", i)
				ec.MarkExhausted(keys[i], 5*time.Minute)
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					ec.IsExhausted(keys[i%size])
					i++
				}
			})
		})
	}
}
