package integration

import (
	"testing"

	"github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestOrganizeKsByDeps(t *testing.T) {

	var kss []v1beta2.Kustomization

	// Apps
	kss = append(kss, v1beta2.Kustomization{
		ObjectMeta: v1.ObjectMeta{
			Name:      "apps",
			Namespace: "flux-system",
		},
		Spec: v1beta2.KustomizationSpec{
			DependsOn: []meta.NamespacedObjectReference{
				{
					Name: "infra-habana",
				},
			},
		},
	})

	// infra-configs
	kss = append(kss, v1beta2.Kustomization{
		ObjectMeta: v1.ObjectMeta{
			Name:      "infra-configs",
			Namespace: "flux-system",
		},
		Spec: v1beta2.KustomizationSpec{
			DependsOn: []meta.NamespacedObjectReference{
				{
					Name: "infra-controllers",
				},
			},
		},
	})

	// infra-controllers
	kss = append(kss, v1beta2.Kustomization{
		ObjectMeta: v1.ObjectMeta{
			Name:      "infra-controllers",
			Namespace: "flux-system",
		},
		Spec: v1beta2.KustomizationSpec{},
	})

	// infra-habana
	kss = append(kss, v1beta2.Kustomization{
		ObjectMeta: v1.ObjectMeta{
			Name:      "infra-habana",
			Namespace: "flux-system",
		},
		Spec: v1beta2.KustomizationSpec{
			DependsOn: []meta.NamespacedObjectReference{
				{
					Name: "infra-configs",
				},
			},
		},
	})

	// infra-users

	kss = append(kss, v1beta2.Kustomization{
		ObjectMeta: v1.ObjectMeta{
			Name:      "infra-users",
			Namespace: "flux-system",
		},
		Spec: v1beta2.KustomizationSpec{
			DependsOn: []meta.NamespacedObjectReference{
				{
					Name: "infra-habana",
				},
			},
		},
	})

	expected := []types.NamespacedName{
		{
			Name:      "infra-controllers",
			Namespace: "flux-system",
		},
		{
			Name:      "infra-configs",
			Namespace: "flux-system",
		},
		{
			Name:      "infra-habana",
			Namespace: "flux-system",
		},
		{
			Name:      "infra-users",
			Namespace: "flux-system",
		},
		{
			Name:      "apps",
			Namespace: "flux-system",
		},
	}
	res := organizeKsByDeps(kss)
	require.Equal(t, expected, res)

}
