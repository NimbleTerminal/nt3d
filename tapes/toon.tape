# The toon demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes -- toon.
Output dist/tapes/toon.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-toon"
Enter
Wait+Screen@60s /fps/
Show
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/toon-1.png
Sleep 0.4s
Screenshot dist/tapes/toon-2.png
Sleep 0.4s
Screenshot dist/tapes/toon-3.png
Sleep 2s
# The same scene shaded smoothly, and back.
Type "t"
Sleep 3s
Type "t"
Sleep 2s
# Four bands, two, and three again.
Type "n"
Sleep 2s
Type "n"
Sleep 2s
Type "n"
Sleep 2s
# Without ink, and with.
Type "b"
Sleep 3s
Type "b"
Sleep 3s
