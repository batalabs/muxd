import SwiftUI

// Glass effect modifier with iOS 26+ liquid glass support
struct GlassInputModifier: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 26.0, *) {
            content
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 24))
        } else {
            content
                .background {
                    RoundedRectangle(cornerRadius: 24)
                        .fill(.ultraThinMaterial)
                        .overlay {
                            RoundedRectangle(cornerRadius: 24)
                                .stroke(Color(.separator), lineWidth: 0.5)
                        }
                }
        }
    }
}

struct ChatGlassModifier: ViewModifier {
    var circular: Bool = false

    func body(content: Content) -> some View {
        if circular {
            if #available(iOS 26.0, *) {
                content
                    .frame(width: 44, height: 44)
                    .glassEffect(.regular, in: .circle)
            } else {
                content
                    .frame(width: 44, height: 44)
                    .background(.ultraThinMaterial, in: Circle())
            }
        } else {
            if #available(iOS 26.0, *) {
                content
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .frame(minHeight: 44)
                    .glassEffect(.regular, in: .capsule)
            } else {
                content
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .frame(minHeight: 44)
                    .background(.ultraThinMaterial, in: Capsule())
            }
        }
    }
}

struct ChatGlassButtonStyle: ButtonStyle {
    var circular: Bool = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .modifier(ChatGlassModifier(circular: circular))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

struct TintedGlassButtonStyle: ButtonStyle {
    var tint: Color = .accentColor

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.title3.weight(.semibold))
            .foregroundColor(.white)
            .frame(width: 44, height: 44)
            .background {
                if #available(iOS 26.0, *) {
                    Circle()
                        .fill(tint.opacity(0.8))
                        .glassEffect(.regular, in: .circle)
                } else {
                    Circle()
                        .fill(tint)
                        .overlay(Circle().stroke(Color.white.opacity(0.3), lineWidth: 1))
                }
            }
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}
