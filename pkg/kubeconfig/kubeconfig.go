/*
Copyright 2023 The KubeStellar Authors.

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

package kubeconfig

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	tenancyv1alpha1 "github.com/kubestellar/kubeflex/api/v1alpha1"
	"github.com/kubestellar/kubeflex/pkg/certs"
	"github.com/kubestellar/kubeflex/pkg/util"
)

func LoadAndMerge(ctx context.Context, client kubernetes.Clientset, name, controlPlaneType string) error {
	cpKonfig, err := loadControlPlaneKubeconfig(ctx, client, name, controlPlaneType)
	if err != nil {
		return err
	}
	adjustConfigKeys(cpKonfig, name, controlPlaneType)

	konfig, err := LoadKubeconfig(ctx)
	if err != nil {
		return err
	}

	err = merge(konfig, cpKonfig)
	if err != nil {
		return err
	}

	return WriteKubeconfig(ctx, konfig)
}

// LoadAndMergeNoWrite: works as LoadAndMerge but on supplied konfig from file and does not write it back
func LoadAndMergeNoWrite(ctx context.Context, client kubernetes.Clientset, name, controlPlaneType string, konfig *clientcmdapi.Config) error {
	cpKonfig, err := loadControlPlaneKubeconfig(ctx, client, name, controlPlaneType)
	if err != nil {
		return err
	}
	adjustConfigKeys(cpKonfig, name, controlPlaneType)

	err = merge(konfig, cpKonfig)
	if err != nil {
		return err
	}

	return nil
}

func loadControlPlaneKubeconfig(ctx context.Context, client kubernetes.Clientset, name, controlPlaneType string) (*clientcmdapi.Config, error) {
	namespace := util.GenerateNamespaceFromControlPlaneName(name)

	ks, err := client.CoreV1().Secrets(namespace).Get(ctx,
		util.GetKubeconfSecretNameByControlPlaneType(controlPlaneType),
		metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	key := util.GetKubeconfSecretKeyNameByControlPlaneType(controlPlaneType)
	return clientcmd.Load(ks.Data[key])
}

func LoadKubeconfig(ctx context.Context) (*clientcmdapi.Config, error) {
	kubeconfig := clientcmd.NewDefaultPathOptions().GetDefaultFilename()
	return clientcmd.LoadFromFile(kubeconfig)
}

func WriteKubeconfig(ctx context.Context, config *clientcmdapi.Config) error {
	kubeconfig := clientcmd.NewDefaultPathOptions().GetDefaultFilename()
	return clientcmd.WriteToFile(*config, kubeconfig)
}

func WatchForSecretCreation(clientset kubernetes.Clientset, controlPlaneName, secretName string) error {
	namespace := util.GenerateNamespaceFromControlPlaneName(controlPlaneName)

	listwatch := cache.NewListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		"secrets",
		namespace,
		fields.Everything(),
	)

	stopCh := make(chan struct{})

	_, controller := cache.NewInformer(
		listwatch,
		&v1.Secret{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				secret := obj.(*v1.Secret)
				if secret.Name == secretName {
					close(stopCh)
				}
			},
		},
	)

	go controller.Run(stopCh)
	<-stopCh
	return nil
}

func adjustConfigKeys(config *clientcmdapi.Config, cpName, controlPlaneType string) {
	switch controlPlaneType {
	case string(tenancyv1alpha1.ControlPlaneTypeOCM):
		renameKey(config.Clusters, "multicluster-controlplane", certs.GenerateClusterName(cpName))
		renameKey(config.AuthInfos, "user", certs.GenerateAuthInfoAdminName(cpName))
		renameKey(config.Contexts, "multicluster-controlplane", certs.GenerateContextName(cpName))
		config.CurrentContext = certs.GenerateContextName(cpName)
		config.Contexts[certs.GenerateContextName(cpName)] = &clientcmdapi.Context{
			Cluster:  certs.GenerateClusterName(cpName),
			AuthInfo: certs.GenerateAuthInfoAdminName(cpName),
		}
	case string(tenancyv1alpha1.ControlPlaneTypeVCluster):
		renameKey(config.Clusters, "my-vcluster", certs.GenerateClusterName(cpName))
		renameKey(config.AuthInfos, "my-vcluster", certs.GenerateAuthInfoAdminName(cpName))
		renameKey(config.Contexts, "my-vcluster", certs.GenerateContextName(cpName))
		config.CurrentContext = certs.GenerateContextName(cpName)
		config.Contexts[certs.GenerateContextName(cpName)] = &clientcmdapi.Context{
			Cluster:  certs.GenerateClusterName(cpName),
			AuthInfo: certs.GenerateAuthInfoAdminName(cpName),
		}
	default:
		return
	}
}

func renameKey(m interface{}, oldKey string, newKey string) interface{} {
	switch v := m.(type) {
	case map[string]*clientcmdapi.Cluster:
		if cluster, ok := v[oldKey]; ok {
			delete(v, oldKey)
			v[newKey] = cluster
		}
	case map[string]*clientcmdapi.AuthInfo:
		if authInfo, ok := v[oldKey]; ok {
			delete(v, oldKey)
			v[newKey] = authInfo
		}
	case map[string]*clientcmdapi.Context:
		if context, ok := v[oldKey]; ok {
			delete(v, oldKey)
			v[newKey] = context
		}
	default:
		// no action
	}
	return m
}
