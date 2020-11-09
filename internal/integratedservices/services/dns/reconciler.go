// Copyright © 2020 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dns

import (
	"context"

	"emperror.dev/errors"
	"github.com/banzaicloud/operator-tools/pkg/reconciler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/banzaicloud/integrated-service-sdk/api/v1alpha1"
	"github.com/banzaicloud/pipeline/internal/integratedservices"
)

// Reconciler decouples creation of kubernetes resources (IS Cr-s
type Reconciler interface {
	// Reconcile creates and applies CRs to a cluster
	Reconcile(ctx context.Context, kubeConfig []byte, config Config, values []byte, spec integratedservices.IntegratedServiceSpec) error
}

// isvcReconciler components struct in charge for assembling the CR manifest  and applying it to a cluster (by delegating to a cluster client)
type isvcReconciler struct {
	scheme *runtime.Scheme
}

// NewISReconciler builds an integrated service reconciler
func NewISReconciler() Reconciler {
	// register needed shemes
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	return isvcReconciler{
		scheme: scheme,
	}
}

func (is isvcReconciler) Reconcile(ctx context.Context, kubeConfig []byte, config Config, values []byte, spec integratedservices.IntegratedServiceSpec) error {
	si := &v1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "external-dns",
		},
		Spec: v1alpha1.ServiceInstanceSpec{
			Service: IntegratedServiceName,
			Version: "",             // TODO comes from the spec?!
			Enabled: nil,            // TODO comes from the spec?!
			Config:  string(values), // TODO to be verifies
		},
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfig)
	if err != nil {
		return errors.Wrap(err, "failed to create rest config from cluster configuration")
	}

	cli, err := client.New(restCfg, client.Options{
		Scheme: is.scheme,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create the client from rest configuration")
	}

	resourceReconciler := reconciler.NewReconcilerWith(cli)
	_, err = resourceReconciler.ReconcileResource(si, reconciler.StateCreated)
	if err != nil {
		return errors.Wrap(err, "failed to reconcile the service instance resource")
	}

	return nil
}
