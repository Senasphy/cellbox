import type { ReactNode } from "react";
import "./globals.css";
import Providers from "./providers";
export const metadata={title:"cellbox",description:"Disposable computers for AI agents"};
export default function RootLayout({children}:{children:ReactNode}){return <html lang="en"><body style={{margin:0,fontFamily:"ui-sans-serif,system-ui",background:"#0b1020",color:"#edf2ff"}}><Providers>{children}</Providers></body></html>}
