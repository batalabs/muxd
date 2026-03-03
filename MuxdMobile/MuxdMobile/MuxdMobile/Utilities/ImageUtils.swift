import UIKit

enum ImageUtils {
    static let maxDimension: CGFloat = 1568
    static let maxFileSize = 1_500_000 // ~1.5 MB

    static func prepareForUpload(_ image: UIImage) -> (data: Data, mediaType: String)? {
        var img = image

        // Scale down if either dimension exceeds max
        let size = img.size
        if size.width > maxDimension || size.height > maxDimension {
            let scale = min(maxDimension / size.width, maxDimension / size.height)
            let newSize = CGSize(width: size.width * scale, height: size.height * scale)
            let renderer = UIGraphicsImageRenderer(size: newSize)
            img = renderer.image { _ in
                img.draw(in: CGRect(origin: .zero, size: newSize))
            }
        }

        // JPEG compress with progressive quality reduction
        for quality in stride(from: 0.85, through: 0.3, by: -0.1) {
            if let data = img.jpegData(compressionQuality: quality),
               data.count <= maxFileSize {
                return (data, "image/jpeg")
            }
        }

        // Last resort: lowest quality
        if let data = img.jpegData(compressionQuality: 0.1) {
            return (data, "image/jpeg")
        }

        return nil
    }
}
