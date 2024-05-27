package benchmark

import (
	"performance/models"
	"performance/utils"
)

func TinyVM() *models.VM {
	return &models.VM{
		Name: utils.RandomName("tiny-vm"),
		Specs: models.VmSpecs{
			CPU:      1,
			RAM:      128,
			DiskSize: 1,
		},
	}
}

func SmallVM() *models.VM {
	return &models.VM{
		Name: utils.RandomName("small-vm"),
		Specs: models.VmSpecs{
			CPU:      1,
			RAM:      1024,
			DiskSize: 5,
		},
	}
}

func MediumVM() *models.VM {
	return &models.VM{
		Name: utils.RandomName("medium-vm"),
		Specs: models.VmSpecs{
			CPU:      2,
			RAM:      2048,
			DiskSize: 10,
		},
	}
}

func LargeVM() *models.VM {
	return &models.VM{
		Name: utils.RandomName("large-vm"),
		Specs: models.VmSpecs{
			CPU:      4,
			RAM:      4096,
			DiskSize: 20,
		},
	}
}
