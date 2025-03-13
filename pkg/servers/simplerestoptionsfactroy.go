// Copyright Contributors to the Open Cluster Management project

package servers

// refer to https://github.com/kubernetes/apiserver/blob/v0.24.11/pkg/server/options/etcd.go#L243

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/storage/value"
	"k8s.io/klog/v2"
)

type SimpleRestOptionsFactory struct {
	Options              genericoptions.EtcdOptions
	TransformerOverrides map[schema.GroupResource]value.Transformer
}

func (f *SimpleRestOptionsFactory) GetRESTOptions(resource schema.GroupResource, example runtime.Object) (generic.RESTOptions, error) {
	ret := generic.RESTOptions{
		StorageConfig:             f.Options.StorageConfig.ForResource(resource),
		Decorator:                 generic.UndecoratedStorage,
		EnableGarbageCollection:   f.Options.EnableGarbageCollection,
		DeleteCollectionWorkers:   f.Options.DeleteCollectionWorkers,
		ResourcePrefix:            resource.Group + "/" + resource.Resource,
		CountMetricPollPeriod:     f.Options.StorageConfig.CountMetricPollPeriod,
		StorageObjectCountTracker: f.Options.StorageConfig.StorageObjectCountTracker,
	}
	if f.TransformerOverrides != nil {
		if transformer, ok := f.TransformerOverrides[resource]; ok {
			ret.StorageConfig.Transformer = transformer
		}
	}
	if f.Options.EnableWatchCache {
		sizes, err := ParseWatchCacheSizes(f.Options.WatchCacheSizes)
		if err != nil {
			return generic.RESTOptions{}, err
		}
		size, ok := sizes[resource]
		if ok && size > 0 {
			klog.Warningf("Dropping watch-cache-size for %v - watchCache size is now dynamic", resource)
		}
		if ok && size <= 0 {
			klog.V(3).Info("Not using watch cache", "resource", resource)
			ret.Decorator = generic.UndecoratedStorage
		} else {
			klog.V(3).Info("Using watch cache", "resource", resource)
			ret.Decorator = genericregistry.StorageWithCacher()
		}
	}
	return ret, nil
}

// ParseWatchCacheSizes turns a list of cache size values into a map of group resources
// to requested sizes.
func ParseWatchCacheSizes(cacheSizes []string) (map[schema.GroupResource]int, error) {
	watchCacheSizes := make(map[schema.GroupResource]int)
	for _, c := range cacheSizes {
		tokens := strings.Split(c, "#")
		if len(tokens) != 2 {
			return nil, fmt.Errorf("invalid value of watch cache size: %s", c)
		}

		size, err := strconv.Atoi(tokens[1])
		if err != nil {
			return nil, fmt.Errorf("invalid size of watch cache size: %s", c)
		}
		if size < 0 {
			return nil, fmt.Errorf("watch cache size cannot be negative: %s", c)
		}
		watchCacheSizes[schema.ParseGroupResource(tokens[0])] = size
	}
	return watchCacheSizes, nil
}
