import SwiftUI

struct ChatRenameView: View {
    @Environment(\.dismiss) private var dismiss
    let title: String
    let onRename: (String) -> Void

    @State private var newTitle: String = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Title", text: $newTitle)
                        .autocapitalization(.sentences)
                } footer: {
                    Text("Enter a new title for this session")
                }
            }
            .navigationTitle("Rename Session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        onRename(newTitle)
                        dismiss()
                    }
                    .disabled(newTitle.isEmpty)
                }
            }
            .onAppear {
                newTitle = title
            }
        }
    }
}

struct AskUserView: View {
    @Environment(\.dismiss) private var dismiss
    let prompt: String
    let onAnswer: (String) -> Void

    @State private var answer = ""

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                Text(prompt)
                    .padding()

                TextField("Your response", text: $answer, axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .lineLimit(3...6)
                    .padding(.horizontal)

                Spacer()
            }
            .navigationTitle("Input Needed")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Skip") {
                        onAnswer("")
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Send") {
                        onAnswer(answer)
                        dismiss()
                    }
                    .disabled(answer.isEmpty)
                }
            }
        }
    }
}
