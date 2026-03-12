import Foundation

struct ShellBootstrapContext: Equatable {
    let projectName: String
    let workingDirectory: String
    let socketPath: String

    static func detect(
        environment: [String: String] = ProcessInfo.processInfo.environment,
        currentDirectoryPath: String = FileManager.default.currentDirectoryPath
    ) -> Self {
        let fallbackProjectName = defaultProjectName(for: currentDirectoryPath)
        let projectName = environment["AETHERFLOW_PROJECT"]?.trimmedNonEmpty ?? fallbackProjectName
        let workingDirectory = environment["AETHERFLOW_WORKING_DIRECTORY"]?.trimmedNonEmpty ?? currentDirectoryPath
        let socketPath = environment["AETHERFLOW_SOCKET_PATH"]?.trimmedNonEmpty ?? defaultSocketPath(for: projectName)
        return Self(projectName: projectName, workingDirectory: workingDirectory, socketPath: socketPath)
    }

    static func defaultProjectName(for currentDirectoryPath: String) -> String {
        let lastComponent = URL(fileURLWithPath: currentDirectoryPath).lastPathComponent.trimmingCharacters(in: .whitespacesAndNewlines)
        if lastComponent.isEmpty || lastComponent == "." || lastComponent == "/" {
            return "aetherflow"
        }
        return lastComponent
    }

    static func defaultSocketPath(for projectName: String) -> String {
        let safeComponent = URL(fileURLWithPath: projectName).lastPathComponent
        if safeComponent.isEmpty || safeComponent == "." || safeComponent == "/" {
            return "/tmp/aetherd.sock"
        }
        return "/tmp/aetherd-\(safeComponent).sock"
    }
}

private extension String {
    var trimmedNonEmpty: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
