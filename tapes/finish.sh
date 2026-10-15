#!/bin/sh
# Copyright (c) 2026 Neomantra Corp.
#
# finish.sh NAME readies what tapes/NAME.tape recorded into dist/tapes/.
#
# The gallery's poster frame, web/assets/NAME.png, is the first of the
# tape's candidates, NAME-1.png to NAME-3.png, that shows the scene: a
# screenshot can land while the scene's Kitty image is being replaced.
#
# The GIF, NAME.gif, loses its leading frames that show no scene: the empty
# terminal ntrecord keeps from before a tape's first Hide, and any frame
# drawn before the Kitty image arrived. Then the rest is compressed hard,
# lossy and in fewer colors: a turning 3D scene changes nearly every pixel
# every frame, which GIF stores poorly.
#
# A frame shows the scene when something below the title line is bright.
# Needs ffmpeg and gifsicle.
set -eu
name=$1
dir=dist/tapes
gif=$dir/$name.gif

# brightness prints, for each frame of an image or GIF, the brightest pixel
# below the title line and above the status line, one frame a line.
brightness() {
	ffmpeg -loglevel error -i "$1" -vf "crop=iw:ih-80:0:40,signalstats,metadata=print:key=lavfi.signalstats.YMAX:file=-" -f null - 2>/dev/null |
		awk -F= '/YMAX/ { print $2 }'
}

poster=
for candidate in "$dir/$name-1.png" "$dir/$name-2.png" "$dir/$name-3.png"; do
	if [ -f "$candidate" ] && [ "$(brightness "$candidate")" -ge 100 ]; then
		poster=$candidate
		break
	fi
done
if [ -z "$poster" ]; then
	echo "finish.sh: no poster candidate for $name shows the scene" >&2
	exit 1
fi
cp "$poster" "web/assets/$name.png"

lead=$(brightness "$gif" | awk '$1 >= 100 { print NR - 1; exit }')
if [ -z "$lead" ]; then
	echo "finish.sh: $gif never shows the scene" >&2
	exit 1
fi
# Frames after the first are drawn over it: unoptimize them before deleting
# it, and optimize what is left.
drop=
if [ "$lead" -gt 0 ]; then
	drop="--delete #0-$((lead - 1))"
fi
# shellcheck disable=SC2086 # drop is two words, or none.
gifsicle -U "$gif" $drop -O3 --lossy=80 --colors 128 -o "$gif.tmp"
mv "$gif.tmp" "$gif"
echo "finish.sh: $name: poster $(basename "$poster"); GIF lost $lead leading frames, now $(du -h "$gif" | cut -f1)"
