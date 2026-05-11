//go:build !linux

package sysinfo

func CollectStatic() StaticInfo { return StaticInfo{} }
func Collect() Info             { return Info{} }
