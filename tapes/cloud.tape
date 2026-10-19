# The point cloud demo: a GIF to share, and the gallery's poster frame.
# Run from the repository root: task tapes.
Output dist/tapes/cloud.gif
Source tapes/common.tape
Hide
Type "bin/nt3d-cloud"
Enter
Wait+Screen@60s /fps/
# The poster shows the default: 300,000 points, clustering on.
Sleep 2s
Show
Sleep 3s
# Poster candidates: finish.sh keeps the first that shows the scene, as
# a screenshot can land while the Kitty image is being replaced.
Screenshot dist/tapes/cloud-1.png
Sleep 0.4s
Screenshot dist/tapes/cloud-2.png
Sleep 0.4s
Screenshot dist/tapes/cloud-3.png
Sleep 2s
# Then a million points, zoomed out until far clusters become single points.
Type "n"
Sleep 3s
Type "----------"
Sleep 3s
Type "----------"
Sleep 3s
# And the same view with the clustering off.
Type "l"
Sleep 3s
