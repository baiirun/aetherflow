import Foundation

struct ShellBootstrapContext: Equatable, Sendable {
    let projectName: String
    let workingDirectory: String
    let daemonURL: String
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
        let daemonURL = environment["AETHERFLOW_DAEMON_URL"]?.trimmedNonEmpty ?? defaultDaemonURL(for: projectName)
        let cliPath = environment["AETHERFLOW_CLI_PATH"]?.trimmedNonEmpty
            ?? defaultCLIPath(for: workingDirectory, pathEnvironment: environment["PATH"])
            ?? "af"
        return Self(projectName: projectName, workingDirectory: workingDirectory, daemonURL: daemonURL, cliPath: cliPath)
    }

    static func defaultProjectName(for currentDirectoryPath: String) -> String {
        let lastComponent = URL(fileURLWithPath: currentDirectoryPath).lastPathComponent.trimmingCharacters(in: .whitespacesAndNewlines)
        if lastComponent.isEmpty || lastComponent == "." || lastComponent == "/" {
            return "aetherflow"
        }
        return lastComponent
    }

    /// Returns the daemon HTTP URL for the given project name, using the same
    /// FNV-1a port-hashing scheme as the Go `protocol.DaemonURLFor` function.
    /// Empty project → "http://127.0.0.1:7070" (the default port).
    /// Non-empty project → "http://127.0.0.1:<7071–7170>" (hashed range).
    static func defaultDaemonURL(for projectName: String) -> String {
        if projectName.isEmpty {
            return "http://127.0.0.1:7070"
        }
        let hash = fnv1a32(projectName)
        let port = 7070 + 1 + Int(hash % 100)
        return "http://127.0.0.1:\(port)"
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

    /// FNV-1a 32-bit hash — matches the Go simpleHash function in protocol/socket.go.
    private static func fnv1a32(_ s: String) -> UInt32 {
        var h: UInt32 = 2166136261
        for byte in s.utf8 {
            h ^= UInt32(byte)
            h = h &* 16777619
        }
        return h
    }
}

private extension String {
    var trimmedNonEmpty: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
