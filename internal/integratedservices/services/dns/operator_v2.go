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
	"encoding/json"

	"emperror.dev/errors"
	"github.com/mitchellh/mapstructure"

	"github.com/banzaicloud/pipeline/internal/common"
	"github.com/banzaicloud/pipeline/internal/integratedservices"
	"github.com/banzaicloud/pipeline/internal/integratedservices/integratedserviceadapter"
	"github.com/banzaicloud/pipeline/internal/integratedservices/services"
	"github.com/banzaicloud/pipeline/internal/integratedservices/services/dns/externaldns"
	"github.com/banzaicloud/pipeline/internal/secret/secrettype"
	"github.com/banzaicloud/pipeline/src/auth"
	"github.com/banzaicloud/pipeline/src/dns/route53"
)

type Operator struct {
	clusterGetter    integratedserviceadapter.ClusterGetter
	clusterService   integratedservices.ClusterService
	orgDomainService OrgDomainService
	secretStore      services.SecretStore
	config           Config
	reconciler       Reconciler
	logger           common.Logger
}

func NewDNSISOperator(
	clusterGetter integratedserviceadapter.ClusterGetter,
	clusterService integratedservices.ClusterService,
	orgDomainService OrgDomainService,
	secretStore services.SecretStore,
	config Config,
	logger common.Logger,
) Operator {
	return Operator{
		clusterGetter:    clusterGetter,
		clusterService:   clusterService,
		orgDomainService: orgDomainService,
		secretStore:      secretStore,
		config:           config,
		reconciler:       reconciler{},
		logger:           logger,
	}
}

func (o Operator) Deactivate(ctx context.Context, clusterID uint, spec integratedservices.IntegratedServiceSpec) error {
	return errors.NewWithDetails("not yet implemented!", "service", o.Name())
}

func (o Operator) Apply(ctx context.Context, clusterID uint, spec integratedservices.IntegratedServiceSpec) error {
	ctx, err := o.ensureOrgIDInContext(ctx, clusterID)
	if err != nil {
		return err
	}

	if err := o.clusterService.CheckClusterReady(ctx, clusterID); err != nil {
		return err
	}

	boundSpec, err := bindIntegratedServiceSpec(spec)
	if err != nil {
		return errors.WrapIf(err, "failed to bind integrated service spec")
	}

	if err := boundSpec.Validate(); err != nil {
		return errors.WrapIf(err, "spec validation failed")
	}

	if boundSpec.ExternalDNS.Provider.Name == dnsBanzai {
		if err := o.orgDomainService.EnsureOrgDomain(ctx, clusterID); err != nil {
			return errors.WrapIf(err, "failed to ensure org domain")
		}
	}

	chartValues, err := o.getChartValues(ctx, clusterID, boundSpec)
	if err != nil {
		return errors.WrapIf(err, "failed to get chart values")
	}

	if rErr := o.reconciler.Reconcile(ctx, clusterID, o.config, chartValues); err != nil {
		return errors.Wrap(rErr, "failed to reconcile the integrated service resource")
	}

	return nil
}

func (o Operator) Name() string {
	return IntegratedServiceName
}

func (o Operator) getChartValues(ctx context.Context, clusterID uint, spec dnsIntegratedServiceSpec) ([]byte, error) {
	cl, err := o.clusterGetter.GetClusterByIDOnly(ctx, clusterID)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get cluster")
	}

	chartValues := externaldns.ChartValues{
		Sources: spec.ExternalDNS.Sources,
		RBAC: &externaldns.RBACSettings{
			Create: cl.RbacEnabled(),
		},
		Image: &externaldns.ImageSettings{
			Repository: o.config.Charts.ExternalDNS.Values.Image.Repository,
			Tag:        o.config.Charts.ExternalDNS.Values.Image.Tag,
		},
		DomainFilters: spec.ExternalDNS.DomainFilters,
		Policy:        string(spec.ExternalDNS.Policy),
		TXTOwnerID:    string(spec.ExternalDNS.TXTOwnerID),
		TXTPrefix:     string(spec.ExternalDNS.TXTPrefix),
		Provider:      getProviderNameForChart(spec.ExternalDNS.Provider.Name),
	}

	if spec.ExternalDNS.Provider.Name == dnsBanzai {
		spec.ExternalDNS.Provider.SecretID = route53.IAMUserAccessKeySecretID
	}

	secretValues, err := o.secretStore.GetSecretValues(ctx, spec.ExternalDNS.Provider.SecretID)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get secret")
	}

	switch spec.ExternalDNS.Provider.Name {
	case dnsBanzai, dnsRoute53:
		chartValues.AWS = &externaldns.AWSSettings{
			Region: secretValues[secrettype.AwsRegion],
			Credentials: &externaldns.AWSCredentials{
				AccessKey: secretValues[secrettype.AwsAccessKeyId],
				SecretKey: secretValues[secrettype.AwsSecretAccessKey],
			},
		}

		if options := spec.ExternalDNS.Provider.Options; options != nil {
			chartValues.AWS.BatchChangeSize = options.BatchChangeSize
			chartValues.AWS.Region = options.Region
		}

	case dnsAzure:
		type azureSecret struct {
			ClientID       string `json:"aadClientId" mapstructure:"AZURE_CLIENT_ID"`
			ClientSecret   string `json:"aadClientSecret" mapstructure:"AZURE_CLIENT_SECRET"`
			TenantID       string `json:"tenantId" mapstructure:"AZURE_TENANT_ID"`
			SubscriptionID string `json:"subscriptionId" mapstructure:"AZURE_SUBSCRIPTION_ID"`
		}

		var secret azureSecret
		if err := mapstructure.Decode(secretValues, &secret); err != nil {
			return nil, errors.WrapIf(err, "failed to decode secret values")
		}

		secretName, err := installSecret(cl, o.config.Namespace, externaldns.AzureSecretName, externaldns.AzureSecretDataKey, secret)
		if err != nil {
			return nil, errors.WrapIfWithDetails(err, "failed to install secret to cluster", "clusterId", clusterID)
		}

		chartValues.Azure = &externaldns.AzureSettings{
			SecretName:    secretName,
			ResourceGroup: spec.ExternalDNS.Provider.Options.AzureResourceGroup,
		}

	case dnsGoogle:
		secretName, err := installSecret(cl, o.config.Namespace, externaldns.GoogleSecretName, externaldns.GoogleSecretDataKey, secretValues)
		if err != nil {
			return nil, errors.WrapIfWithDetails(err, "failed to install secret to cluster", "clusterId", clusterID)
		}

		chartValues.Google = &externaldns.GoogleSettings{
			Project:              secretValues[secrettype.ProjectId],
			ServiceAccountSecret: secretName,
		}

		if options := spec.ExternalDNS.Provider.Options; options != nil {
			chartValues.Google.Project = options.GoogleProject
		}

	default:
	}

	rawValues, err := json.Marshal(chartValues)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to marshal chart values")
	}

	return rawValues, nil
}

func (o Operator) ensureOrgIDInContext(ctx context.Context, clusterID uint) (context.Context, error) {
	if _, ok := auth.GetCurrentOrganizationID(ctx); !ok {
		cluster, err := o.clusterGetter.GetClusterByIDOnly(ctx, clusterID)
		if err != nil {
			return ctx, errors.WrapIf(err, "failed to get cluster by ID")
		}
		ctx = auth.SetCurrentOrganizationID(ctx, cluster.GetOrganizationId())
	}
	return ctx, nil
}

// Reconciler decouples creation of kubernetes resources (IS Cr-s
type Reconciler interface {
	// TODO finalize the interface
	// Reconcile creates and applies CRs to a cluster
	Reconcile(ctx context.Context, clusterID uint, config Config, values []byte) error
}

// reconciler components struct in charge for assembling the CR manifest  and applying it to a cluster (by delegating to a cluster client)
type reconciler struct {
}

func (r reconciler) Reconcile(ctx context.Context, clusterID uint, config Config, values []byte) error {
	// TODO assemble the CR instance based on the spec, config and values
	// TODO use a k8s client instance to apply the resource to the cluster

	return errors.NewWithDetails("not yet implemented!")
}
