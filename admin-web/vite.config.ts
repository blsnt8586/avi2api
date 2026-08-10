import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
export default defineConfig({
  plugins:[react(),tailwindcss()],
  build:{
    outDir:'../internal/web/dist',
    emptyOutDir:true,
    rollupOptions:{
      output:{
        manualChunks(id){
          if(!id.includes('node_modules')) return
          if(id.includes('@tanstack/react-query')) return 'query-runtime'
          if(id.includes('/react-router/') || id.includes('/react-router-dom/')) return 'router-runtime'
          if(/node_modules[\\/](react|react-dom|scheduler)[\\/]/.test(id)) return 'react-runtime'
        },
      },
    },
  },
})
