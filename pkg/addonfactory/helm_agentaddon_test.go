package addonfactory

import (
	"embed"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1apha1 "open-cluster-management.io/api/cluster/v1alpha1"
)

//go:embed testmanifests/chart
//go:embed testmanifests/chart/templates/_helpers.tpl
var chartFS embed.FS

type config struct {
	OverrideName string
	IsHubCluster bool
	Global       global
}

type global struct {
	ImagePullPolicy string
	ImagePullSecret string
	ImageOverrides  map[string]string
	NodeSelector    map[string]string
	ProxyConfig     map[string]string
}

func getValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
	userConfig := config{
		OverrideName: addon.Name,
		Global: global{
			ImagePullPolicy: "Always",
			ImagePullSecret: "mySecret",
			ImageOverrides: map[string]string{
				"testImage": "quay.io/testImage:dev",
			},
		},
	}
	if cluster.GetName() == "local-cluster" {
		userConfig.IsHubCluster = true
	}

	return StructToValues(userConfig), nil
}

func TestChartAgentAddon_Manifests(t *testing.T) {
	testScheme := runtime.NewScheme()
	_ = clusterv1apha1.Install(testScheme)
	_ = apiextensionsv1.AddToScheme(testScheme)
	_ = apiextensionsv1beta1.AddToScheme(testScheme)
	_ = scheme.AddToScheme(testScheme)

	cases := []struct {
		name                     string
		scheme                   *runtime.Scheme
		clusterName              string
		addonName                string
		installNamespace         string
		annotationValues         string
		expectedInstallNamespace string
		expectedNodeSelector     map[string]string
		expectedImage            string
		expectedObjCnt           int
	}{
		{
			name:                     "template render ok with annotation values",
			scheme:                   testScheme,
			clusterName:              "cluster1",
			addonName:                "helloworld",
			installNamespace:         "myNs",
			annotationValues:         `{"global": {"nodeSelector":{"host":"ssd"},"imageOverrides":{"testImage":"quay.io/helloworld:2.4"}}}`,
			expectedInstallNamespace: "myNs",
			expectedNodeSelector:     map[string]string{"host": "ssd"},
			expectedImage:            "quay.io/helloworld:2.4",
			expectedObjCnt:           4,
		},
		{
			name:                     "template render ok with empty yaml",
			scheme:                   testScheme,
			clusterName:              "local-cluster",
			addonName:                "helloworld",
			installNamespace:         "myNs",
			annotationValues:         `{"global": {"nodeSelector":{"host":"ssd"},"imageOverrides":{"testImage":"quay.io/helloworld:2.4"}}}`,
			expectedInstallNamespace: "myNs",
			expectedNodeSelector:     map[string]string{"host": "ssd"},
			expectedImage:            "quay.io/helloworld:2.4",
			expectedObjCnt:           2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cluster := NewFakeManagedCluster(c.clusterName)
			clusterAddon := NewFakeManagedClusterAddon(c.addonName, c.clusterName, c.installNamespace, c.annotationValues)

			agentAddon, err := NewAgentAddonFactory(c.addonName, chartFS, "testmanifests/chart").
				WithGetValuesFuncs(getValues, GetValuesFromAddonAnnotation).
				WithScheme(c.scheme).
				BuildHelmAgentAddon()
			if err != nil {
				t.Errorf("expected no error, got err %v", err)
			}
			objects, err := agentAddon.Manifests(cluster, clusterAddon)
			if err != nil {
				t.Errorf("expected no error, got err %v", err)
			}

			if len(objects) != c.expectedObjCnt {
				t.Errorf("expected %v objects,but got %v", c.expectedObjCnt, len(objects))
			}
			for _, o := range objects {
				switch object := o.(type) {
				case *appsv1.Deployment:
					if object.Namespace != c.expectedInstallNamespace {
						t.Errorf("expected namespace is %s, but got %s", c.expectedInstallNamespace, object.Namespace)
					}

					nodeSelector := object.Spec.Template.Spec.NodeSelector
					for k, v := range c.expectedNodeSelector {
						if nodeSelector[k] != v {
							t.Errorf("expected nodeSelector is %v, but got %v", c.expectedNodeSelector, nodeSelector)
						}
					}

					if object.Spec.Template.Spec.Containers[0].Image != c.expectedImage {
						t.Errorf("expected Image is %s, but got %s", c.expectedImage, object.Spec.Template.Spec.Containers[0].Image)
					}
				case *clusterv1apha1.ClusterClaim:
					if object.Spec.Value != c.clusterName {
						t.Errorf("expected clusterName is %s, but got %s", c.clusterName, object.Spec.Value)
					}
				case *apiextensionsv1.CustomResourceDefinition:
					if object.Name != "test.cluster.open-cluster-management.io" {
						t.Errorf("expected v1 crd test, but got %v", object.Name)
					}
				case *apiextensionsv1beta1.CustomResourceDefinition:
					if object.Name != "clusterclaims.cluster.open-cluster-management.io" {
						t.Errorf("expected v1 crd clusterclaims, but got %v", object.Name)
					}
				}

			}
		})
	}
}
