# The campath demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes -- campath.
Output dist/tapes/campath.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-campath"
Enter
Wait+Screen@60s /fov/
Show
Sleep 2s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/campath-1.png
Sleep 0.4s
Screenshot dist/tapes/campath-2.png
Sleep 0.4s
Screenshot dist/tapes/campath-3.png
Sleep 2.5s
# Detach to director orbit view to inspect the live 3D camera frustum & spline.
Type "k"
Sleep 3.5s
# Cycle to closed orbit with focal-length pull, then camera switcher, and ride again.
Type "]"
Sleep 2.5s
Type "]"
Sleep 2.5s
Type "k"
Sleep 3s
