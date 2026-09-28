import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuth } from '../stores/auth'
const routes: RouteRecordRaw[]=[
{path:'/login',component:()=>import('../views/Login.vue')},
{path:'/',component:()=>import('../layouts/AppLayout.vue'),children:[
{path:'',redirect:'/dashboard'},
{path:'dashboard',component:()=>import('../views/Dashboard.vue')},
{path:'resources',component:()=>import('../views/Resources.vue')},
{path:'monitor',component:()=>import('../views/Monitor.vue')},
{path:'accounts',component:()=>import('../views/Accounts.vue')},
{path:'system/users',component:()=>import('../views/Users.vue'),meta:{admin:true}},
{path:'system/audit',component:()=>import('../views/Audit.vue'),meta:{admin:true}}
]},{path:'/:pathMatch(.*)*',redirect:'/dashboard'}]
const router=createRouter({history:createWebHistory(),routes})
router.beforeEach(async to=>{const auth=useAuth();if(to.path==='/login')return true;if(!localStorage.getItem('token'))return '/login';if(!auth.loaded)await auth.load();if(to.meta.admin&&!auth.admin)return '/dashboard';return true})
export default router
