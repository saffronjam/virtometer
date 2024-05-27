package models

type VM struct {
	Name  string
	Specs VmSpecs
}

type VmSpecs struct {
	// CPU is the number of CPU cores
	CPU int `json:"cpu"`
	// RAM is the amount of RAM in MB
	RAM int `json:"ram"`
	// DiskSize is the size of the disk in GB
	DiskSize int `json:"diskSize"`
}
