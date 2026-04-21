# epub2zip

A robust Go command-line utility to extract sequential images from fixed-layout EPUBs (illustrated and art books) and archive them into standard ZIP files.

`epub2zip` is designed specifically for fixed-layout digital books, ensuring that the physical page layout (Left-to-Right or Right-to-Left) and alignment are preserved.

## Key Features

- **Fixed-Layout Intelligence**: Automatically detects whether an EPUB is fixed-layout or reflowable. Prevents accidental processing of text-only books.
- **Japanese Layout Support**: Robust handling of `rtl` (Right-to-Left) and `ltr` (Left-to-Right) reading directions.
- **Spread Alignment**: Automatically inserts alignment blanks based on EPUB spread metadata (`page-spread-left/right`) to ensure images land on the correct physical side in double-page readers.
- **Multi-Image Pages**: Handles logical pages that contain multiple image files, extracting them with `[PAGENO]_[INDEX]` naming to ensure no assets are lost.
- **Metadata Export**: Optionally extracts book metadata (title, author, revision, identifiers) into a root `metadata.json` file.
- **Batch Processing**: Process multiple files at once with internal glob/wildcard support (works on Windows CMD/PowerShell).
- **Smart Blanks**: Calculates dimensions for alignment blanks by comparing neighboring page areas to maintain visual consistency.

## Installation

```bash
go install github.com/mixcode/epub2zip@latest
```
*(Or clone the repository and run `go build`)*

## Usage

### Basic Conversion
Convert an EPUB to a ZIP in the current directory:
```bash
epub2zip book.epub
```

### Batch Processing
Convert all EPUBs in a folder and save them to a specific directory:
```bash
mkdir -p archive
epub2zip -o archive example_epub/*.epub
```

### Metadata Options
Include a compact `metadata.json` in the resulting ZIP:
```bash
epub2zip -m compact book.epub
```

### Custom Alignment Blanks
Generate alignment pages with a specific color (named or hex):
```bash
epub2zip -b generate --blank-color "#1a1a1a" book.epub
```

## CLI Flags

| Flag | Description | Default |
| :--- | :--- | :--- |
| `-o` | Output filename or directory | Current Dir |
| `-p` | Filename padding size (e.g., `-p 3` -> `001.jpg`) | `4` |
| `-v` | Enable verbose logging | `false` |
| `-d` | Dry run: list pages without creating the ZIP | `false` |
| `-b` | Blank page handling: `skip` or `generate` | `generate` |
| `--blank-color` | Color for blanks: `white`, `black`, `transparent`, or `#HEX` | `transparent` |
| `-m` | Metadata JSON mode: `none`, `compact`, `pretty` | `pretty` |
| `-f` | Force execution on reflowable books | `false` |

## Alignment Logic

`epub2zip` implements standard Japanese EPUB layout rules:
- **RTL Books**: Odd pages are on the **Left**, Even pages are on the **Right**.
- **LTR Books**: Odd pages are on the **Right**, Even pages are on the **Left**.

If a page's metadata indicates it must be on a specific side (e.g., a "right" spread page), the tool checks if the current sequence number matches that side. If not, it automatically inserts an alignment blank to push the image to the correct position.

## License

MIT License. See [LICENSE](LICENSE) for details.
