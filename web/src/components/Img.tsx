import { useState } from "react";
import type { ImgHTMLAttributes } from "react";

/** A lazy image that fades in once decoded, so posters don't pop in
 * half-painted. The container behind it shows the loading state. */
export function Img({ className = "", ...props }: ImgHTMLAttributes<HTMLImageElement>) {
  const [loaded, setLoaded] = useState(false);
  return (
    <img
      alt=""
      loading="lazy"
      decoding="async"
      draggable={false}
      {...props}
      ref={(el) => {
        if (el?.complete && el.naturalWidth > 0) setLoaded(true);
      }}
      onLoad={() => setLoaded(true)}
      className={`${className} transition-opacity duration-500 ${loaded ? "opacity-100" : "opacity-0"}`}
    />
  );
}
