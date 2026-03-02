// Simple Swift function examples

import Foundation

// 1. Basic function - no parameters, no return
func sayHello() {
    print("Hello, World!")
}

// 2. Function with parameters
func greet(name: String) {
    print("Hello, \(name)!")
}

// 3. Function with return value
func add(a: Int, b: Int) -> Int {
    return a + b
}

// 4. Function with multiple returns (tuple)
func minMax(numbers: [Int]) -> (min: Int, max: Int)? {
    guard !numbers.isEmpty else { return nil }
    return (numbers.min()!, numbers.max()!)
}

// 5. Function with default parameter
func greetUser(name: String, greeting: String = "Hello") {
    print("\(greeting), \(name)!")
}

// 6. Function with variadic parameters
func sum(_ numbers: Int...) -> Int {
    numbers.reduce(0, +)
}

// 7. Function with inout parameter (modifies the original)
func doubleInPlace(_ value: inout Int) {
    value *= 2
}

// 8. Function as a closure (shorthand)
let square: (Int) -> Int = { $0 * $0 }

// 9. Higher-order function (takes another function)
func applyOperation(_ a: Int, _ b: Int, operation: (Int, Int) -> Int) -> Int {
    return operation(a, b)
}

// 10. Async function
func fetchData(from url: String) async throws -> String {
    // Simulated async work
    try await Task.sleep(nanoseconds: 100_000_000)
    return "Data from \(url)"
}

// MARK: - Usage Examples

// Call basic function
sayHello()

// Call with argument
greet(name: "Rui")

// Use return value
let result = add(a: 5, b: 3)
print("5 + 3 = \(result)")

// Multiple returns
if let bounds = minMax(numbers: [10, 3, 8, 1, 15]) {
    print("Min: \(bounds.min), Max: \(bounds.max)")
}

// Default parameter
greetUser(name: "Anna")
greetUser(name: "Bob", greeting: "Hi")

// Variadic
let total = sum(1, 2, 3, 4, 5)
print("Sum: \(total)")

// Inout
var number = 10
doubleInPlace(&number)
print("Doubled: \(number)")

// Closure
let nine = square(3)
print("3² = \(nine)")

// Higher-order
let multiply = applyOperation(4, 5) { $0 * $1 }
print("4 × 5 = \(multiply)")

// Async (would run in an async context)
// Task {
//     let data = try await fetchData(from: "https://example.com")
//     print(data)
// }
