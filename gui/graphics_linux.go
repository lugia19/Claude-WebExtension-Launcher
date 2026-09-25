package gui

// defaultGraphicsAPI is used unless GOGPU_GRAPHICS_API is set. On Linux that's
// OpenGL ES: gogpu's Vulkan path left stale and black areas under llvmpipe (VMs,
// machines without GPU drivers), while GLES drew correctly, and EGL/GLES is available
// on practically every Linux desktop, including ones without Vulkan drivers.
const defaultGraphicsAPI = "gles"
