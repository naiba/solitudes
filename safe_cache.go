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
	List map[string]*sync.Cond
}

// GetOrBuild 获取或重建缓存
func (sc *SafeCache) GetOrBuild(key string, build func() (interface{}, error)) (interface{}, error) {
	if v, has := System.Cache.Get(key); has {
		return v, nil
	}
	// 查询是否已在重建
	var loading, has bool
	var cond *sync.Cond
	sc.Lock()
	if cond, has = sc.List[key]; has {
		loading = true
	} else {
		cond = sync.NewCond(new(sync.Mutex))
		sc.List[key] = cond
	}
	sc.Unlock()

	// 重建缓存，并通知订阅者
	var v interface{}
	var err error
	if !loading {
		// 如果是重建携程，重建后删除 key
		defer func() {
			sc.Lock()
			sc.List[key] = nil
			delete(sc.List, key)
			sc.Unlock()
		}()
		// 重建缓存
		v, err = build()
		if err == nil {
			System.Cache.Set(key, v, cache.DefaultExpiration)
		}
		// 通知其他请求
		cond.Broadcast()
		return v, err
	}

	// 接收重建通知
	done := make(chan struct{})
	System.Pool.Submit(func() {
		cond.Wait()
		close(done)
	})
	select {
	case <-time.After(time.Second * 5):
		return nil, errors.New("Error get cache time out")
	case <-done:
		if err != nil {
			return nil, err
		}
		return v, err
	}
}
