# The lights demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/lights.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-lights"
Enter
Wait+Screen@60s /fps/
# The poster shows the last preset, eight lights in a ring, two presets
# on from the opening lamps.
Type "ll"
Sleep 1s
Show
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/lights-1.png
Sleep 0.4s
Screenshot dist/tapes/lights-2.png
Sleep 0.4s
Screenshot dist/tapes/lights-3.png
Sleep 2s
# Then a tour of the others, from the top: sky and key, sky IBL, image
# IBL, and the SH studio.
Type "l"
Sleep 2s
Type "l"
Sleep 3s
Type "l"
Sleep 4s
Type "l"
Sleep 4s
