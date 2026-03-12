import Foundation

struct ShellBootstrapContext: Equatable, Sendable {
    let projectName: String
    let workingDirectory: String
    let socketPath: String
    let cliPath: String

    static func detect(
        environment: [String: String] = ProcessInfo.processInfo.environment,
        currentDirectoryPath: String = FileManager.default.currentDirectoryPath
    ) -> Self {
        let workingDirectory = environment["AETHERFLOW_WORKING_DIRECTORY"]?.trimmedNonEmpty ?? currentDirectoryPath
        let fallbackProjectName = defaultProjectName(for: workingDirectory)
        let configuredProjectName = configuredProjectName(for: workingDirectory)
        let projectName = environment["AETHERFLOW_PROJECT"]?.trimmedNonEmpty
            ?? configuredProjectName
            ?? fallbackProjectName
        let socketPath = environment["AETHERFLOW_SOCKET_PATH"]?.trimmedNonEmpty ?? defaultSocketPath(for: projectName)
        let cliPath = environment["AETHERFLOW_CLI_PATH"]?.trimmedNonEmpty
            ?? defaultCLIPath(for: workingDirectory, pathEnvironment: environment["PATH"])
            ?? "af"
        return Self(projectName: projectName, workingDirectory: workingDirectory, socketPath: socketPath, cliPath: cliPath)
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

    static func defaultCLIPath(for workingDirectory: String, pathEnvironment: String?) -> String? {
        let repoBinary = URL(fileURLWithPath: workingDirectory).appendingPathComponent("af").path
        if FileManager.default.isExecutableFile(atPath: repoBinary) {
            return repoBinary
        }

        guard let pathEnvironment else {
            return nil
        }
        for pathEntry in pathEnvironment.split(separator: ":") {
            let candidate = URL(fileURLWithPath: String(pathEntry)).appendingPathComponent("af").path
            if FileManager.default.isExecutableFile(atPath: candidate) {
                return candidate
            }
        }
        return nil
    }

    static func configuredProjectName(for workingDirectory: String) -> String? {
        let configPath = URL(fileURLWithPath: workingDirectory).appendingPathComponent(".aetherflow.yaml").path
        guard let data = FileManager.default.contents(atPath: configPath),
              let contents = String(data: data, encoding: .utf8) else {
            return nil
        }

        for line in contents.split(whereSeparator: \.isNewline) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard trimmed.hasPrefix("project:") else {
                continue
            }
            let value = trimmed.dropFirst("project:".count).trimmingCharacters(in: .whitespaces)
            let unquoted = value.trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
            return unquoted.isEmpty ? nil : unquoted
        }
        return nil
    }
}

private extension String {
    var trimmedNonEmpty: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
