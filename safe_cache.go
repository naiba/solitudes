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
	// 查询是否已在重建
	var loading bool
	var ch chan error
	sc.Lock()
	if _, ok := sc.List[key]; ok {
		loading = true
		ch = make(chan error)
	}
	sc.List[key] = append(sc.List[key], ch)
	sc.Unlock()

	// 重建缓存，并通知订阅者
	var v interface{}
	var err error
	if !loading {
		// 如果是重建携程，重建后删除 key
		defer func() {
			sc.Lock()
			delete(sc.List, key)
			sc.Unlock()
		}()
		// 重建缓存
		v, err = build()
		if err == nil {
			System.Cache.Set(key, v, cache.DefaultExpiration)
		}
		// 通知其他请求
		for i := 0; i < len(sc.List[key]); i++ {
			select {
			case sc.List[key][i] <- err:
			default:
			}
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
