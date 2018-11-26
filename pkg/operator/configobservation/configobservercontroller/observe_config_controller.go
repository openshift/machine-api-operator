package configobservercontroller

import (
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/library-go/pkg/operator/configobserver"
	machineapioperatorinformers "github.com/openshift/machine-api-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/machine-api-operator/pkg/operator/configobservation"
)

type ConfigObserver struct {
	*configobserver.ConfigObserver
}

// NewConfigObserver initializes a new configuration observer.
func NewConfigObserver(
	operatorClient configobserver.OperatorClient,
	operatorConfigInformers machineapioperatorinformers.SharedInformerFactory,
) *ConfigObserver {
	// FUTURE: this would let us observe other sources of config to register for event handling beyond our CR
	c := &ConfigObserver{
		ConfigObserver: configobserver.NewConfigObserver(
			operatorClient,
			configobservation.Listers{
				PreRunCachesSynced: []cache.InformerSynced{
					operatorConfigInformers.Machineapi().V1alpha1().MachineAPIOperatorConfigs().Informer().HasSynced,
				},
			},
		),
	}
	operatorConfigInformers.Machineapi().V1alpha1().MachineAPIOperatorConfigs().Informer().AddEventHandler(c.EventHandler())
	return c
}
