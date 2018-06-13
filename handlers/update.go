package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/openfaas/faas/gateway/requests"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// MakeUpdateHandler update specified function
func MakeUpdateHandler(functionNamespace string, clientset *kubernetes.Clientset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		defer r.Body.Close()

		body, _ := ioutil.ReadAll(r.Body)

		request := requests.CreateFunctionRequest{}
		err := json.Unmarshal(body, &request)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		getOpts := metav1.GetOptions{}

		deployment, findDeployErr := clientset.ExtensionsV1beta1().
			Deployments(functionNamespace).
			Get(request.Service, getOpts)

		if findDeployErr != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(findDeployErr.Error()))
			return
		}

		if len(deployment.Spec.Template.Spec.Containers) > 0 {
			deployment.Spec.Template.Spec.Containers[0].Image = request.Image

			// Disabling update support to prevent unexpected mutations of deployed functions,
			// since imagePullPolicy is now configurable. This could be reconsidered later depending
			// on desired behavior, but will need to be updated to take config.
			//deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = v1.PullAlways

			deployment.Spec.Template.Spec.Containers[0].Env = buildEnvVars(&request)

			deployment.Spec.Template.Spec.NodeSelector = createSelector(request.Constraints)

			labels := map[string]string{
				"faas_function": request.Service,
				"uid":           fmt.Sprintf("%d", time.Now().Nanosecond()),
			}

			if request.Labels != nil {
				if min := getMinReplicaCount(*request.Labels); min != nil {
					deployment.Spec.Replicas = min
				}

				for k, v := range *request.Labels {
					labels[k] = v
				}
			}

			deployment.Labels = labels
			deployment.Spec.Template.ObjectMeta.Labels = labels

			resources, resourceErr := createResources(request)
			if resourceErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(resourceErr.Error()))
				return
			}

			deployment.Spec.Template.Spec.Containers[0].Resources = *resources

			existingSecrets, err := getSecrets(clientset, functionNamespace, request.Secrets)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(err.Error()))
				return
			}

			err = UpdateSecrets(request, deployment, existingSecrets)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(err.Error()))
				return
			}
		}

		if _, updateErr := clientset.ExtensionsV1beta1().
			Deployments(functionNamespace).
			Update(deployment); updateErr != nil {

			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(updateErr.Error()))
		}
	}
}
