package configobservation

import (
	"k8s.io/client-go/tools/cache"
)

type Listers struct {
	PreRunCachesSynced []cache.InformerSynced
}

func (l Listers) PreRunHasSynced() []cache.InformerSynced {
	return l.PreRunCachesSynced
}
