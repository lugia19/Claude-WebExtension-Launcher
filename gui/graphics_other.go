//go:build !linux

package gui

// defaultGraphicsAPI is used unless GOGPU_GRAPHICS_API is set; empty lets gogpu pick
// (DX12/Vulkan on Windows, Metal on macOS). See graphics_linux.go.
const defaultGraphicsAPI = ""
