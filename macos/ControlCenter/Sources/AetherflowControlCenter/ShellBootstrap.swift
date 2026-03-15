import Foundation

struct ShellBootstrapContext: Equatable, Sendable {
    let projectName: String
    let workingDirectory: String
    let daemonURL: String
    let cliPath: String
    let daemonTargetReason: String
    let daemonListenAddressOverride: String?

    init(
        projectName: String,
        workingDirectory: String,
        daemonURL: String,
        cliPath: String,
        daemonTargetReason: String = "Defaulting to the global manual daemon endpoint.",
        daemonListenAddressOverride: String? = nil
    ) {
        self.projectName = projectName
        self.workingDirectory = workingDirectory
        self.daemonURL = daemonURL
        self.cliPath = cliPath
        self.daemonTargetReason = daemonTargetReason
        self.daemonListenAddressOverride = daemonListenAddressOverride ?? Self.listenAddrFromDaemonURL(daemonURL)
    }

    static func detect(
        environment: [String: String] = ProcessInfo.processInfo.environment,
        currentDirectoryPath: String = FileManager.default.currentDirectoryPath
    ) -> Self {
        let workingDirectory = environment["AETHERFLOW_WORKING_DIRECTORY"]?.trimmedNonEmpty ?? currentDirectoryPath
        let fallbackProjectName = defaultProjectName(for: workingDirectory)
        let configuredValues = configuredValues(for: workingDirectory)
        let configuredProjectName = configuredValues.projectName
        let projectName = environment["AETHERFLOW_PROJECT"]?.trimmedNonEmpty
            ?? configuredProjectName
            ?? fallbackProjectName
        let daemonTarget = resolvedDaemonTarget(
            environmentDaemonURL: environment["AETHERFLOW_DAEMON_URL"],
            configuredValues: configuredValues
        )
        let cliPath = environment["AETHERFLOW_CLI_PATH"]?.trimmedNonEmpty
            ?? defaultCLIPath(for: workingDirectory, pathEnvironment: environment["PATH"])
            ?? "af"
        return Self(
            projectName: projectName,
            workingDirectory: workingDirectory,
            daemonURL: daemonTarget.url,
            cliPath: cliPath,
            daemonTargetReason: daemonTarget.reason,
            daemonListenAddressOverride: daemonTarget.listenAddress
        )
    }

    static func defaultProjectName(for currentDirectoryPath: String) -> String {
        let lastComponent = URL(fileURLWithPath: currentDirectoryPath).lastPathComponent.trimmingCharacters(in: .whitespacesAndNewlines)
        if lastComponent.isEmpty || lastComponent == "." || lastComponent == "/" {
            return "aetherflow"
        }
        return lastComponent
    }

    static func defaultManualDaemonURL() -> String {
        "http://127.0.0.1:7070"
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

    static func configuredValues(for workingDirectory: String) -> (projectName: String?, daemonURL: String?, listenAddress: String?) {
        let configPath = URL(fileURLWithPath: workingDirectory).appendingPathComponent(".aetherflow.yaml").path
        guard let data = FileManager.default.contents(atPath: configPath),
              let contents = String(data: data, encoding: .utf8) else {
            return (nil, nil, nil)
        }

        var projectName: String?
        var daemonURL: String?
        var listenAddress: String?
        for line in contents.split(whereSeparator: \.isNewline) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("project:") {
                let value = trimmed.dropFirst("project:".count).trimmingCharacters(in: .whitespaces)
                let unquoted = value.trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
                projectName = unquoted.isEmpty ? nil : unquoted
            }
            if trimmed.hasPrefix("listen_addr:") {
                let value = trimmed.dropFirst("listen_addr:".count).trimmingCharacters(in: .whitespaces)
                let unquoted = value.trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
                if let target = daemonTargetFromListenAddr(unquoted) {
                    listenAddress = target.listenAddress
                    daemonURL = target.url
                }
            }
        }
        return (projectName, daemonURL, listenAddress)
    }

    static func daemonTargetFromListenAddr(_ listenAddr: String) -> (url: String, listenAddress: String)? {
        let trimmed = listenAddr.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            return nil
        }

        let normalizedListenAddr = trimmed.hasPrefix(":") ? "127.0.0.1\(trimmed)" : trimmed
        let candidateURL = "http://\(normalizedListenAddr)"
        guard let daemonURL = validatedLoopbackDaemonURL(candidateURL),
              let normalizedListenAddress = listenAddrFromDaemonURL(daemonURL) else {
            return nil
        }
        return (daemonURL, normalizedListenAddress)
    }

    static func listenAddrFromDaemonURL(_ rawValue: String) -> String? {
        guard let rawValue = validatedLoopbackDaemonURL(rawValue),
              let url = URL(string: rawValue),
              let host = url.host,
              let port = url.port else {
            return nil
        }
        if host == "::1" {
            return "[::1]:\(port)"
        }
        return "\(host):\(port)"
    }

    private static func resolvedDaemonTarget(
        environmentDaemonURL: String?,
        configuredValues: (projectName: String?, daemonURL: String?, listenAddress: String?)
    ) -> (url: String, reason: String, listenAddress: String?) {
        if let daemonURL = validatedLoopbackDaemonURL(environmentDaemonURL),
           let listenAddress = listenAddrFromDaemonURL(daemonURL) {
            return (
                daemonURL,
                "Using the explicit AETHERFLOW_DAEMON_URL override for manual daemon monitoring.",
                listenAddress
            )
        }
        if let daemonURL = configuredValues.daemonURL,
           let listenAddress = configuredValues.listenAddress?.trimmedNonEmpty {
            return (
                daemonURL,
                "Using listen_addr from .aetherflow.yaml for manual daemon monitoring.",
                listenAddress
            )
        }
        let defaultURL = defaultManualDaemonURL()
        return (
            defaultURL,
            "No daemon override was provided, so the app is targeting the global manual daemon endpoint.",
            listenAddrFromDaemonURL(defaultURL)
        )
    }
}

private extension String {
    var trimmedNonEmpty: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}

private func validatedLoopbackDaemonURL(_ rawValue: String?) -> String? {
    guard let rawValue = rawValue?.trimmingCharacters(in: .whitespacesAndNewlines),
          let url = URL(string: rawValue),
          url.scheme == "http",
          let host = url.host?.lowercased(),
          host == "127.0.0.1" || host == "localhost" || host == "::1" else {
        return nil
    }
    return rawValue
}
