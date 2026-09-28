import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { get, post } from '../api'
export const useAuth = defineStore('auth',()=>{const user=ref<any>(null),demo=ref(false),loaded=ref(false);const admin=computed(()=>user.value?.role==='admin');async function load(){if(!localStorage.getItem('token'))return;try{const r=await get('/auth/me');user.value=r.user;demo.value=r.demo;loaded.value=true}catch{user.value=null}}
async function login(username:string,password:string){const r=await post('/auth/login',{username,password});localStorage.setItem('token',r.token);user.value=r.user;demo.value=r.demo;loaded.value=true}
function logout(){localStorage.removeItem('token');user.value=null;location.href='/login'}
return{user,demo,loaded,admin,load,login,logout}})
