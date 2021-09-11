package propagate

import (
	"bytes"
	"context"
	_ "embed"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

//go:embed schema.yaml
var schemaYAML string

var schemaTypeMap = map[schema.GroupVersionKind]string{
	schema.GroupVersionKind{Group: appsv1.GroupName, Version: appsv1.SchemeGroupVersion.Version, Kind: "Deployment"}: "io.k8s.api.apps.v1.Deployment",
}

func NewParser() *typed.Parser {
	parser, err := typed.NewParser(typed.YAMLObject(schemaYAML))
	if err != nil {
		panic(err)
	}
	return parser
}

func extractFields(ctx context.Context, parser *typed.Parser, res *unstructured.Unstructured, filter func(mf metav1.ManagedFieldsEntry) bool) (map[string]interface{}, error) {
	logger := log.FromContext(ctx)

	fieldset := &fieldpath.Set{}
	objManagedFields := res.GetManagedFields()
	for _, mf := range objManagedFields {
		if !filter(mf) {
			continue
		}
		fs := &fieldpath.Set{}
		err := fs.FromJSON(bytes.NewReader(mf.FieldsV1.Raw))
		if err != nil {
			return nil, err
		}
		fieldset = fieldset.Union(fs)
	}
	logger.Info("managedFields", "managedFields", objManagedFields)

	var d *typed.TypedValue
	if v, ok := schemaTypeMap[res.GroupVersionKind()]; ok {
		logger.V(5).Info("structured", "gvk", res.GroupVersionKind(), "key", v)
		var err error
		d, err = parser.Type(v).FromStructured(res)
		if err != nil {
			return nil, err
		}
	} else {
		logger.V(5).Info("unstructured", "gvk", res.GroupVersionKind())
		var err error
		d, err = typed.DeducedParseableType.FromUnstructured(res.Object)
		if err != nil {
			return nil, err
		}
	}

	items := d.ExtractItems(fieldset.Leaves()).AsValue().Unstructured()
	m, ok := items.(map[string]interface{})
	if !ok {
		panic("cannot cast")
	}
	logger.V(5).Info("extractFields", "items", items)
	return m, nil
}
