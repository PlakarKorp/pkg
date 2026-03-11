package pkg

import "slices"

type ResourceClass string

type ResourceSubClass string

const (
	ResourceClassUndefined     ResourceClass = ""
	ResourceClassUnknown       ResourceClass = "unknown"
	ResourceClassCompute       ResourceClass = "compute"
	ResourceClassDatabase      ResourceClass = "database"
	ResourceClassBlockStorage  ResourceClass = "block-storage"
	ResourceClassObjectStorage ResourceClass = "object-storage"
	ResourceClassAPI           ResourceClass = "api"

	ResourceSubClassUndefined ResourceSubClass = ""
	ResourceSubClassUnknown   ResourceSubClass = "unknown"
	ResourceSubClassS3        ResourceSubClass = "s3"
	ResourceSubClassGCS       ResourceSubClass = "gcs"
)

var resourceClasses []ResourceClass = []ResourceClass{
	ResourceClassUndefined,
	ResourceClassUnknown,
	ResourceClassCompute,
	ResourceClassDatabase,
	ResourceClassBlockStorage,
	ResourceClassObjectStorage,
	ResourceClassAPI,
}

var resourceSubClasses []ResourceSubClass = []ResourceSubClass{
	ResourceSubClassUndefined,
	ResourceSubClassUnknown,
	ResourceSubClassS3,
	ResourceSubClassGCS,
}

func (c ResourceClass) IsValid() bool {
	return slices.Contains(resourceClasses, c)
}

func (c ResourceSubClass) IsValid() bool {
	return slices.Contains(resourceSubClasses, c)
}
