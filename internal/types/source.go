package types

// Annotation keys a federated event-exporter attaches to an event to record
// where it originated: plane type, cluster, region, and city.
const (
	SourcePlaneTypeAnnotation = "activity.miloapis.com/source-plane-type"
	SourceClusterAnnotation   = "activity.miloapis.com/source-cluster"
	SourceRegionAnnotation    = "activity.miloapis.com/source-region"
	SourceCityAnnotation      = "activity.miloapis.com/source-city"
)
