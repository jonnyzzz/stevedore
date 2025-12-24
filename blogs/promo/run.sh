#!/bin/bash
set -e

# Build the image generator container
docker build -t stevedore-promo-gen ./blogs/promo

# Run it, mounting the blogs/promo directory to output the files
# We mount current dir's blogs/promo to /app/blogs/promo inside because the script writes to blogs/promo
# Actually, the script writes to "blogs/promo/...", so we need to run it from the root or adjust the script.
# Let's adjust the mount so it works simply.

echo "Generating images..."
docker run --rm \
  -v "$(pwd)/blogs/promo:/app/blogs/promo" \
  stevedore-promo-gen

echo "Done! Images are in blogs/promo/"

