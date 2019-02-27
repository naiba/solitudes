package solitudes

import (
	"errors"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

// SafeCache 防穿透防雪崩的缓存
type SafeCache struct {
	sync.Mutex
	List map[string][]chan error
}

// GetOrBuild 获取或重建缓存
func (sc *SafeCache) GetOrBuild(key string, build func() (interface{}, error)) (interface{}, error) {
	if v, has := System.Cache.Get(key); has {
		return v, nil
	}
	// 如果是重建携程，重建后删除 key
	var loading bool
	defer func() {
		if !loading {
			sc.Lock()
			delete(sc.List, key)
			sc.Unlock()
		}
	}()

	// 查询是否已在重建
	var ch chan error
	sc.Lock()
	if _, ok := sc.List[key]; ok {
		loading = true
		ch = make(chan error)
		sc.List[key] = append(sc.List[key], ch)
	}
	sc.Unlock()

	// 重建缓存，并通知订阅者
	var v interface{}
	var err error
	if !loading {
		v, err = build()
		if err == nil {
			System.Cache.Set(key, v, cache.DefaultExpiration)
		}
		for i := 0; i < len(sc.List[key]); i++ {
			sc.List[key][i] <- err
		}
		return v, err
	}

	// 接收重建通知
	select {
	case err := <-ch:
		if err != nil {
			return nil, err
		}
		v, _ = System.Cache.Get(key)
		return v, err
	case <-time.After(time.Second * 5):
		return nil, errors.New("Error cache load time out")
	}
}
