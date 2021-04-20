/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package util

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// NewDefaultWithLazyFallbackRESTMapper creates a rest mapper from the scheme.Scheme which will
// fall back to a lazy dynamic rest mapper if an object is not registered in the scheme.
func NewDefaultWithLazyFallbackRESTMapper(cfg *rest.Config) (meta.RESTMapper, error) {
	return NewDefaultWithLazyFallbackRESTMapperProviderFromScheme(scheme.Scheme)(cfg)
}

// NewDefaultWithLazyFallbackRESTMapperProviderFromScheme creates a rest mapper provider from the scheme which will
// fall back to a lazy dynamic rest mapper if an object is not registered in the scheme.
func NewDefaultWithLazyFallbackRESTMapperProviderFromScheme(sch *runtime.Scheme) func(*rest.Config) (meta.RESTMapper, error) {
	return func(cfg *rest.Config) (meta.RESTMapper, error) {
		lazyDynamicRestMapper, err := apiutil.NewDynamicRESTMapper(cfg, apiutil.WithLazyDiscovery)
		if err != nil {
			return nil, fmt.Errorf("could not initialise dynamic rest mapper: %v", err)
		}

		mapper := meta.NewDefaultRESTMapper(sch.PreferredVersionAllGroups())
		return meta.FirstHitRESTMapper{
			MultiRESTMapper: meta.MultiRESTMapper{
				mapper,
				lazyDynamicRestMapper,
			},
		}, nil
	}
}
