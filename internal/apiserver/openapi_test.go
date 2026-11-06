package apiserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiopenapi "k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	restclient "k8s.io/client-go/rest"
	basecompatibility "k8s.io/component-base/compatibility"
	"k8s.io/kube-openapi/pkg/handler3"

	"go.miloapis.com/activity/internal/registry/activity/eventfacet"
	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	"go.miloapis.com/activity/pkg/generated/openapi"
)

func newOpenAPITestServer(t *testing.T) *ActivityServer {
	t.Helper()

	config := genericapiserver.NewConfig(Codecs)
	config.EffectiveVersion = basecompatibility.NewEffectiveVersionFromString("1.34", "", "")
	config.ExternalAddress = "127.0.0.1:443"
	config.LoopbackClientConfig = &restclient.Config{}
	config.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(openapi.GetOpenAPIDefinitionsWithUnstructured, apiopenapi.NewDefinitionNamer(Scheme))

	genericServer, err := config.Complete(nil).New("activity-apiserver-test", genericapiserver.NewEmptyDelegate())
	require.NoError(t, err)
	s := &ActivityServer{GenericAPIServer: genericServer}

	activityGroup := genericapiserver.NewDefaultAPIGroupInfo(v1alpha1.GroupName, Scheme, metav1.ParameterCodec, Codecs)
	activityGroup.VersionedResourcesStorageMap["v1alpha1"] = map[string]rest.Storage{
		"eventfacetqueries": eventfacet.NewEventFacetQueryStorage(nil),
	}
	require.NoError(t, genericServer.InstallAPIGroup(&activityGroup))

	eventsBackend := storage.NewClickHouseEventsBackend(nil, storage.ClickHouseEventsConfig{})
	require.NoError(t, s.installLegacyCoreAPIGroup(eventsBackend))
	require.NoError(t, s.installEventsAPIGroup(eventsBackend))

	return s
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

// An aggregator merges every group-version a backend lists in /openapi/v3 and picks between backends that list the
// same one at random, so this server must list only the group its APIService registers.
func TestOpenAPIV3ListsOnlyTheActivityGroup(t *testing.T) {
	s := newOpenAPITestServer(t)
	s.prepareRun()
	handler := s.GenericAPIServer.Handler

	response := get(t, handler, "/openapi/v3")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var index handler3.OpenAPIV3Discovery
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &index))

	require.Contains(t, index.Paths, "apis/activity.miloapis.com")
	require.Contains(t, index.Paths, "apis/activity.miloapis.com/v1alpha1")
	for _, borrowed := range []string{"api", "api/v1", "apis", "apis/events.k8s.io", "apis/events.k8s.io/v1", "version"} {
		require.NotContains(t, index.Paths, borrowed)
		require.Empty(t, get(t, handler, "/openapi/v3/"+borrowed).Body.Bytes(), borrowed)
	}
	require.NotEmpty(t, get(t, handler, "/openapi/v3/apis/activity.miloapis.com/v1alpha1").Body.Bytes())

	for _, path := range []string{"/api/v1", "/apis/events.k8s.io/v1"} {
		response := get(t, handler, path)
		require.Equal(t, http.StatusOK, response.Code, path)
		var resources metav1.APIResourceList
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &resources), path)
		var names []string
		for _, resource := range resources.APIResources {
			names = append(names, resource.Name)
		}
		require.Contains(t, names, "events", path)
	}
}
