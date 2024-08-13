points = X0;
previousZ = points(1,2);
G = zeros(size(points));
index = 1;
figure;
hold;
xlim([-4 4])
ylim([-2,10])
zlim([0 5])
for i=1:numel(points(:,1))
   if points(i,2) == previousZ
       G(index,:) = points(i,:);
       index  = index + 1;
   else

       plot3(G(1:index-1,1), G(1:index-1,2), G(1:index-1,3))
       index = 1;
       G(index,:) = points(i,:);
       previousZ = points(i,2);
   end
end
plot3(G(1:index-1,1), G(1:index-1,2), G(1:index-1,3))