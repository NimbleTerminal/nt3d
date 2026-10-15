# The mipmaps demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/mipmaps.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-mipmaps"
Enter
Wait+Screen@60s /fps/
Sleep 1s
Show
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/mipmaps-1.png
Sleep 0.4s
Screenshot dist/tapes/mipmaps-2.png
Sleep 0.4s
Screenshot dist/tapes/mipmaps-3.png
Type "m"
Sleep 3s
Type "m"
Sleep 3s
