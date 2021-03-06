package installation

import (
	"encoding/base64"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_GetMasterHash(t *testing.T) {
	i := Installation{}
	h, err := i.getMasterHash()
	require.NoError(t, err)
	require.True(t, isHex(h))
}

func Test_GetLatestAvailableMasterHash(t *testing.T) {
	i := Installation{
		Options: &Options{
			FallbackLevel: 5,
		},
	}
	h, err := i.getLatestAvailableMasterHash()
	require.NoError(t, err)
	require.True(t, isHex(h))
}

func Test_GetInstallerImage(t *testing.T) {
	const image = "eu.gcr.io/kyma-project/kyma-installer:63f27f76"
	testData := File{Content: []map[string]interface{}{{
		"apiVersion": "installer.kyma-project.io/v1alpha1",
		"kind":       "Deployment",
		"spec": map[interface{}]interface{}{
			"template": map[interface{}]interface{}{
				"spec": map[interface{}]interface{}{
					"serviceAccountName": "kyma-installer",
					"containers": []interface{}{
						map[interface{}]interface{}{
							"name":  "kyma-installer-container",
							"image": image,
						},
					},
				},
			},
		},
	},
	},
	}

	insImage, err := getInstallerImage(&testData)
	require.NoError(t, err)
	require.Equal(t, image, insImage)
}

func Test_ReplaceDockerImageURL(t *testing.T) {
	const replacedWithData = "testImage!"
	testData := []struct {
		testName       string
		data           File
		expectedResult File
		shouldFail     bool
	}{
		{
			testName: "correct data test",
			data: File{Content: []map[string]interface{}{{
				"apiVersion": "installer.kyma-project.io/v1alpha1",
				"kind":       "Deployment",
				"spec": map[interface{}]interface{}{
					"template": map[interface{}]interface{}{
						"spec": map[interface{}]interface{}{
							"serviceAccountName": "kyma-installer",
							"containers": []interface{}{
								map[interface{}]interface{}{
									"name":  "kyma-installer-container",
									"image": "eu.gcr.io/kyma-project/kyma-installer:63f27f76",
								},
							},
						},
					},
				},
			},
			},
			},
			expectedResult: File{Content: []map[string]interface{}{
				{
					"apiVersion": "installer.kyma-project.io/v1alpha1",
					"kind":       "Deployment",
					"spec": map[interface{}]interface{}{
						"template": map[interface{}]interface{}{
							"spec": map[interface{}]interface{}{
								"serviceAccountName": "kyma-installer",
								"containers": []interface{}{
									map[interface{}]interface{}{
										"name":  "kyma-installer-container",
										"image": replacedWithData,
									},
								},
							},
						},
					},
				},
			},
			},
			shouldFail: false,
		},
	}

	for _, tt := range testData {
		err := replaceInstallerImage(&tt.data, replacedWithData)
		if !tt.shouldFail {
			require.Nil(t, err, tt.testName)
			require.Equal(t, tt.data, tt.expectedResult, tt.testName)
		} else {
			require.NotNil(t, err, tt.testName)
		}
	}
}

func Test_LoadConfigurations(t *testing.T) {
	domain := "test.kyma"
	tlsCert := "testCert"
	tlsKey := "testKey"
	password := "testPass"

	installation := &Installation{
		Options: &Options{
			OverrideConfigs: []string{path.Join("../../internal/testdata", "overrides.yaml")},
			IsLocal:         false,
			Domain:          domain,
			TLSCert:         tlsCert,
			TLSKey:          tlsKey,
			Password:        password,
		},
	}

	configurations, err := installation.loadConfigurations(nil)
	require.NoError(t, err)
	require.Equal(t, 4, len(configurations.Configuration))
	dom, ok := configurations.Configuration.Get("global.domainName")
	require.Equal(t, true, ok)
	require.Equal(t, domain, dom.Value)
	tlsC, ok := configurations.Configuration.Get("global.tlsCrt")
	require.Equal(t, true, ok)
	require.Equal(t, tlsCert, tlsC.Value)
	tlsK, ok := configurations.Configuration.Get("global.tlsKey")
	require.Equal(t, true, ok)
	require.Equal(t, tlsKey, tlsK.Value)
	pass, ok := configurations.Configuration.Get("global.adminPassword")
	require.Equal(t, true, ok)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte(password)), pass.Value)

	require.Equal(t, 1, len(configurations.ComponentConfiguration))
	require.Equal(t, "istio", configurations.ComponentConfiguration[0].Component)
	cpuR, ok := configurations.ComponentConfiguration[0].Configuration.Get("global.proxy.resources.requests.cpu")
	require.Equal(t, true, ok)
	require.Equal(t, "490m", cpuR.Value)
	memR, ok := configurations.ComponentConfiguration[0].Configuration.Get("global.proxy.resources.requests.memory")
	require.Equal(t, true, ok)
	require.Equal(t, "127Mi", memR.Value)
	cpuL, ok := configurations.ComponentConfiguration[0].Configuration.Get("global.proxy.resources.limits.cpu")
	require.Equal(t, true, ok)
	require.Equal(t, "499m", cpuL.Value)
	memL, ok := configurations.ComponentConfiguration[0].Configuration.Get("global.proxy.resources.limits.memory")
	require.Equal(t, true, ok)
	require.Equal(t, "1023Mi", memL.Value)
}

func Test_LoadComponentsConfig(t *testing.T) {
	installation := &Installation{
		Options: &Options{
			ComponentsConfig: path.Join("../../internal/testdata", "components.yaml"),
		},
	}

	components, err := installation.loadComponentsConfig()
	require.NoError(t, err)
	require.Equal(t, 6, len(components))

	installation2 := &Installation{
		Options: &Options{
			ComponentsConfig: path.Join("../../internal/testdata", "installationCR.yaml"),
		},
	}

	components, err = installation2.loadComponentsConfig()
	require.NoError(t, err)
	require.Equal(t, 8, len(components))
}
