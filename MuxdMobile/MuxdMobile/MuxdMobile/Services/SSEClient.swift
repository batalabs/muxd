import Foundation

final class SSEClient: NSObject, URLSessionDataDelegate, @unchecked Sendable {
    private let baseURL: URL
    private let authToken: String
    private var task: URLSessionDataTask?
    private var buffer = Data()
    private let lock = NSLock()

    var onEvent: (@Sendable (SSEEvent) -> Void)?
    var onComplete: (@Sendable () -> Void)?
    var onError: (@Sendable (Error) -> Void)?

    init(host: String, port: Int, token: String) {
        self.baseURL = URL(string: "http://\(host):\(port)")!
        self.authToken = token
        super.init()
    }

    func submit(sessionID: String, text: String) {
        let url = baseURL.appendingPathComponent("/api/sessions/\(sessionID)/submit")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(authToken)", forHTTPHeaderField: "Authorization")
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")

        let body = ["text": text]
        request.httpBody = try? JSONEncoder().encode(body)

        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 300 // 5 min for long operations
        let session = URLSession(configuration: config, delegate: self, delegateQueue: nil)

        lock.lock()
        buffer = Data()
        lock.unlock()

        task = session.dataTask(with: request)
        task?.resume()
    }

    func cancel() {
        task?.cancel()
        task = nil
    }

    // MARK: - URLSessionDataDelegate

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        lock.lock()
        buffer.append(data)
        let currentBuffer = buffer
        lock.unlock()

        processBuffer(currentBuffer)
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        if let error = error {
            // Ignore cancellation errors
            if (error as NSError).code == NSURLErrorCancelled {
                return
            }
            let callback = onError
            DispatchQueue.main.async { callback?(error) }
        } else {
            let callback = onComplete
            DispatchQueue.main.async { callback?() }
        }
    }

    private func processBuffer(_ currentBuffer: Data) {
        guard let string = String(data: currentBuffer, encoding: .utf8) else { return }

        // Split by double newlines (SSE event delimiter)
        let eventBlocks = string.components(separatedBy: "\n\n")

        // Keep incomplete event in buffer
        lock.lock()
        if !string.hasSuffix("\n\n") && eventBlocks.count > 1 {
            buffer = (eventBlocks.last ?? "").data(using: .utf8) ?? Data()
        } else if string.hasSuffix("\n\n") {
            buffer = Data()
        }
        lock.unlock()

        // Process complete events
        let completeBlocks = string.hasSuffix("\n\n") ? eventBlocks : Array(eventBlocks.dropLast())

        for block in completeBlocks {
            if let event = parseEventBlock(block) {
                let callback = onEvent
                DispatchQueue.main.async { callback?(event) }
            }
        }
    }

    private func parseEventBlock(_ block: String) -> SSEEvent? {
        var eventType: String?
        var dataString: String?

        for line in block.components(separatedBy: "\n") {
            if line.hasPrefix("event: ") {
                eventType = String(line.dropFirst(7))
            } else if line.hasPrefix("data: ") {
                dataString = String(line.dropFirst(6))
            }
        }

        guard let type = eventType,
              let data = dataString?.data(using: .utf8) else {
            return nil
        }

        return SSEEvent.parse(eventType: type, data: data)
    }
}
