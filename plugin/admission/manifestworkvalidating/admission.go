// Copyright Contributors to the Open Cluster Management project
package manifestworkvalidating

import (
	"context"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apiserver/pkg/admission"
	genericadmissioninitializer "k8s.io/apiserver/pkg/admission/initializer"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/generic"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/request"
	"k8s.io/client-go/kubernetes"
	workv1 "open-cluster-management.io/api/work/v1"
	workwebhookv1 "open-cluster-management.io/ocm/pkg/work/webhook/v1"
	runtimeadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const PluginName = "ManifestWorkValidating"

func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(config io.Reader) (admission.Interface, error) {
		return NewPlugin(), nil
	})
}

type Plugin struct {
	*admission.Handler
	webhook *workwebhookv1.ManifestWorkWebhook
}

func (p *Plugin) SetExternalKubeClientSet(client kubernetes.Interface) {
	p.webhook.SetExternalKubeClientSet(client)
}

func (p *Plugin) ValidateInitialization() error {
	if p.webhook == nil {
		return fmt.Errorf("missing webhook")
	}
	return nil
}

var _ admission.ValidationInterface = &Plugin{}
var _ admission.InitializationValidator = &Plugin{}
var _ = genericadmissioninitializer.WantsExternalKubeClientSet(&Plugin{})

func NewPlugin() *Plugin {
	return &Plugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
		webhook: &workwebhookv1.ManifestWorkWebhook{},
	}
}

func (p *Plugin) Validate(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
	v := admission.VersionedAttributes{
		Attributes:         a,
		VersionedOldObject: a.GetOldObject(),
		VersionedObject:    a.GetObject(),
		VersionedKind:      a.GetKind(),
	}

	gvr := workv1.GroupVersion.WithResource("manifestworks")
	gvk := workv1.GroupVersion.WithKind("ManifestWork")

	// resource is not work
	if a.GetKind() != gvk {
		return nil
	}

	// don't set kind cause do not use it in code logical
	i := generic.WebhookInvocation{
		Resource: gvr,
		Kind:     gvk,
	}

	uid := types.UID(uuid.NewUUID())
	ar := request.CreateV1AdmissionReview(uid, &v, &i)

	r := runtimeadmission.Request{AdmissionRequest: *ar.Request}
	admissionContext := runtimeadmission.NewContextWithRequest(ctx, r)

	work := &workv1.ManifestWork{}
	obj := a.GetObject().(*unstructured.Unstructured)
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, work)
	if err != nil {
		return err
	}

	switch a.GetOperation() {
	case admission.Create:
		_, err := p.webhook.ValidateCreate(admissionContext, work)
		return err
	case admission.Update:
		oldWork := &workv1.ManifestWork{}
		oldObj := a.GetOldObject().(*unstructured.Unstructured)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(oldObj.Object, oldWork)
		if err != nil {
			return err
		}
		_, err = p.webhook.ValidateUpdate(admissionContext, oldWork, work)
		return err
	}

	return nil
}
