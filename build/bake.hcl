group "default" {
  targets = ["test"]
}

target "base" {
  context    = ".."
  dockerfile = "build/Dockerfile"
  args = {
    GATEWAY_SOURCE_COMMIT = ""
    AWG_GO_COMMIT    = "cf9d2dd202821301f7039093b0a1b3d4b574c47c"
    AWG_TOOLS_COMMIT = "d09ecc38425082e472368dd2bf8c4c42d10cae03"
  }
}

target "test" {
  inherits  = ["base"]
  target    = "test"
  platforms = ["linux/amd64"]
}

target "image-amd64" {
  inherits  = ["base"]
  target    = "runtime"
  platforms = ["linux/amd64"]
  output    = ["type=docker"]
}

target "image-arm64" {
  inherits  = ["base"]
  target    = "runtime"
  platforms = ["linux/arm64"]
  output    = ["type=oci,dest=build/evidence/awg3-routeros-gateway-arm64.oci.tar"]
}

target "image-armv5" {
  inherits  = ["base"]
  target    = "runtime"
  platforms = ["linux/arm/v5"]
  output    = ["type=oci,dest=build/evidence/awg3-routeros-gateway-armv5.oci.tar"]
}

target "image-multiarch" {
  inherits  = ["base"]
  target    = "runtime"
  platforms = ["linux/amd64", "linux/arm64", "linux/arm/v5"]
  output    = ["type=oci,dest=build/evidence/awg3-routeros-gateway.oci.tar"]
}

target "smoke" {
  inherits  = ["base"]
  target    = "smoke"
  platforms = ["linux/amd64"]
  output    = ["type=docker"]
}
