package catalog

// Resource-attribute namespace classification.
//
// Records interleave two kinds of attribute: the event's own payload (a log's
// content/loglevel, a span's http.*/db.*/exception.*) and *resource* context
// that describes where the signal came from (host.*, k8s.*, cloud.*, the
// language runtime, …). When triaging a single record the payload is what
// someone reads first; the resource context is background. The record
// inspector uses this to sink recognized resource namespaces below the payload
// groups (but above dt.* plumbing) — see groupRank in internal/tui.
//
// This is a *demotion hint*, never a taxonomy: it only ever nudges ordering,
// is never surfaced in the UI, and is deliberately conservative. Grail does
// not mark OTLP resource attributes, so a custom/non-well-known resource
// attribute is invisible to us — it simply stays with the payload (prominent),
// which is the safe failure. We never hide or label anything.
//
// The set is derived from the Dynatrace semantic dictionary's resource attributes
// (docs.dynatrace.com/docs/semantic-dictionary), which are OpenTelemetry-aligned, so
// the classification tracks OTel resource conventions without a runtime
// dependency on the dictionary. First-dot namespace tokens only, matching how
// the inspector groups fields.
//
// Namespaces that straddle both resource and signal definitions upstream are
// resolved by dominant use: aws/azure/cloud/faas/process/device/browser/
// os_service lean resource (canonical OTel resource conventions) and are
// included; http is excluded (http.request.method / http.response.status_code
// are span payload, not context); dt.* is excluded here because it is handled
// as its own plumbing tier; cics/zos are excluded as niche and genuinely mixed.
var resourceNamespaces = map[string]struct{}{
	// OTel-core resource + infrastructure.
	"host": {}, "k8s": {}, "cloud": {}, "container": {}, "container_image": {},
	"deployment": {}, "faas": {}, "process": {}, "service": {}, "os": {},
	"os_service": {}, "device": {}, "browser": {}, "app": {}, "geo": {},
	"telemetry": {}, "disk": {}, "storage": {}, "network_interface": {},
	"hypervisor": {}, "detection": {}, "primary_tags": {},
	// Cloud providers.
	"aws": {}, "azure": {}, "gcp": {},
	// Language runtimes / technology stacks.
	"java": {}, "dotnet": {}, "nodejs": {}, "go": {}, "php": {}, "aspnetcore": {},
	"apache": {}, "iis": {}, "jboss": {}, "weblogic": {}, "websphere": {},
	"websphere_liberty": {}, "glassfish": {}, "spring": {}, "cassandra": {},
	"elasticsearch": {}, "tibco": {}, "coldfusion": {}, "hybris": {}, "ibm": {},
	"softwareag": {}, "equinox": {}, "servlet": {}, "snmp": {}, "bosh": {},
	"cloudfoundry": {}, "openstack": {}, "vmware": {}, "adobe": {}, "esb": {},
}

// ResourceNamespace reports whether a first-dot field namespace describes
// resource context rather than event payload. Used only to order the record
// inspector's attribute groups; see the package var doc for the rationale.
func ResourceNamespace(namespace string) bool {
	_, ok := resourceNamespaces[namespace]
	return ok
}
