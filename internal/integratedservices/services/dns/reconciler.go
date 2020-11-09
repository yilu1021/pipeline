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
)

// Reconciler decouples creation of kubernetes resources (IS Cr-s
type Reconciler interface {
	// TODO finalize the interface
	// Reconcile creates and applies CRs to a cluster
	Reconcile(ctx context.Context, clusterID uint, config Config, values []byte) error
}

// reconciler components struct in charge for assembling the CR manifest  and applying it to a cluster (by delegating to a cluster client)
type isReconciler struct {
	reconciler reconciler.ResourceReconciler
}

func (is isReconciler) Reconcile(ctx context.Context, clusterID uint, config Config, values []byte) error {
	is.reconciler.CreateIfNotExist()

	return errors.NewWithDetails("not yet implemented!")
}
