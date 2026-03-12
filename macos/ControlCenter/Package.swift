// swift-tools-version: 6.2

import PackageDescription

let package = Package(
    name: "AetherflowControlCenter",
    platforms: [
        .macOS(.v14),
    ],
    products: [
        .executable(
            name: "AetherflowControlCenter",
            targets: ["AetherflowControlCenter"]
        ),
    ],
    targets: [
        .executableTarget(
            name: "AetherflowControlCenter"
        ),
        .testTarget(
            name: "AetherflowControlCenterTests",
            dependencies: ["AetherflowControlCenter"]
        ),
    ]
)
